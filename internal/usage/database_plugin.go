package usage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// DatabaseOptions controls persistence of usage statistics.
type DatabaseOptions struct {
	Enabled       bool
	Path          string
	RetentionDays int
}

type databasePlugin struct{}

var (
	currentUsageStore atomic.Pointer[usageStore]
	currentDBConfig   atomic.Pointer[DatabaseOptions]
)

func init() {
	coreusage.RegisterPlugin(databasePlugin{})
}

// ConfigureDatabase wires the on-disk usage store based on options.
func ConfigureDatabase(opts DatabaseOptions) error {
	normalized := normalizeDatabaseOptions(opts)
	prev := currentDBConfig.Load()
	if configsEqual(prev, &normalized) {
		return nil
	}

	if !normalized.Enabled || normalized.Path == "" {
		currentDBConfig.Store(&normalized)
		prevStore := currentUsageStore.Swap(nil)
		if prevStore != nil {
			prevStore.close()
		}
		return nil
	}

	store, err := newUsageStore(normalized)
	if err != nil {
		return err
	}
	old := currentUsageStore.Swap(store)
	if old != nil {
		old.close()
	}
	currentDBConfig.Store(&normalized)
	return nil
}

func normalizeDatabaseOptions(opts DatabaseOptions) DatabaseOptions {
	if opts.RetentionDays <= 0 {
		opts.RetentionDays = 14
	}
	if opts.Path != "" {
		opts.Path = filepath.Clean(opts.Path)
	}
	return opts
}

func configsEqual(a, b *DatabaseOptions) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Enabled == b.Enabled &&
		a.Path == b.Path &&
		a.RetentionDays == b.RetentionDays
}

func (databasePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	store := currentUsageStore.Load()
	if store == nil {
		return
	}

	detail := normaliseDetail(record.Detail)
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	status := resolveStatusCode(ctx)
	rateLimited := status == http.StatusTooManyRequests
	apiKeyHash := fingerprint(record.APIKey)

	dbRec := dbRecord{
		Timestamp:             timestamp.UTC(),
		Provider:              record.Provider,
		Model:                 record.Model,
		CredentialLabel:       credentialLabel(record),
		CredentialFingerprint: credentialFingerprint(record),
		APIKeyHash:            apiKeyHash,
		AuthID:                record.AuthID,
		AuthIndex:             record.AuthIndex,
		Source:                record.Source,
		StatusCode:            status,
		Failed:                record.Failed,
		RateLimited:           rateLimited,
		Tokens:                detail,
	}

	if err := store.enqueue(dbRec); err != nil {
		log.WithError(err).Warn("usage: failed to persist usage record")
	}
}

func resolveStatusCode(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return 0
	}
	return ginCtx.Writer.Status()
}

func credentialLabel(record coreusage.Record) string {
	if record.AuthID != "" {
		return record.AuthID
	}
	if record.Source != "" {
		return record.Source
	}
	if record.Provider != "" {
		return record.Provider
	}
	if record.APIKey != "" {
		return "api-key"
	}
	return "unknown"
}

func credentialFingerprint(record coreusage.Record) string {
	switch {
	case record.AuthID != "":
		return fingerprint(record.AuthID)
	case record.Source != "":
		return fingerprint(record.Source)
	case record.APIKey != "":
		return fingerprint(record.APIKey)
	case record.Provider != "":
		return fingerprint(record.Provider)
	default:
		return fingerprint("unknown")
	}
}

func fingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type dbRecord struct {
	Timestamp             time.Time
	Provider              string
	Model                 string
	CredentialLabel       string
	CredentialFingerprint string
	APIKeyHash            string
	AuthID                string
	AuthIndex             uint64
	Source                string
	StatusCode            int
	Failed                bool
	RateLimited           bool
	Tokens                TokenStats
}

type usageStore struct {
	db            *sql.DB
	retentionDays int
	queue         chan dbRecord
	stop          chan struct{}
	wg            sync.WaitGroup
}

func newUsageStore(opts DatabaseOptions) (*usageStore, error) {
	if opts.Path == "" {
		return nil, errors.New("usage: database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return nil, fmt.Errorf("usage: mkdir failed: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout=5000&_pragma=foreign_keys=on", filepath.ToSlash(opts.Path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("usage: open sqlite: %w", err)
	}
	if err := applyUsageSchema(db); err != nil {
		return nil, err
	}

	store := &usageStore{
		db:            db,
		retentionDays: opts.RetentionDays,
		queue:         make(chan dbRecord, 2048),
		stop:          make(chan struct{}),
	}
	store.wg.Add(2)
	go store.run()
	go store.retentionLoop()
	return store, nil
}

func applyUsageSchema(db *sql.DB) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS usage_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			provider TEXT,
			model TEXT,
			credential_label TEXT,
			credential_fingerprint TEXT,
			api_key_hash TEXT,
			auth_id TEXT,
			auth_index INTEGER,
			source TEXT,
			status_code INTEGER,
			failed INTEGER,
			rate_limited INTEGER,
			prompt_tokens INTEGER,
			completion_tokens INTEGER,
			reasoning_tokens INTEGER,
			cached_tokens INTEGER,
			total_tokens INTEGER
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_requests_provider_time ON usage_requests(provider, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_requests_fingerprint ON usage_requests(credential_fingerprint, timestamp);`,
		`CREATE TABLE IF NOT EXISTS usage_daily (
			day TEXT NOT NULL,
			provider TEXT NOT NULL,
			credential_fingerprint TEXT NOT NULL,
			credential_label TEXT NOT NULL,
			model TEXT NOT NULL,
			total_requests INTEGER NOT NULL,
			failed_requests INTEGER NOT NULL,
			rate_limited INTEGER NOT NULL,
			prompt_tokens INTEGER NOT NULL,
			completion_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			PRIMARY KEY (day, provider, credential_fingerprint, model)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_daily_provider ON usage_daily(provider, day);`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("usage: apply schema: %w", err)
		}
	}
	return nil
}

func (s *usageStore) enqueue(rec dbRecord) error {
	select {
	case s.queue <- rec:
		return nil
	case <-s.stop:
		return errors.New("usage: database store stopped")
	}
}

func (s *usageStore) run() {
	defer s.wg.Done()
	for {
		select {
		case rec := <-s.queue:
			if err := s.insert(rec); err != nil {
				log.WithError(err).Warn("usage: insert failed")
			}
		case <-s.stop:
			s.drainRemaining()
			return
		}
	}
}

func (s *usageStore) drainRemaining() {
	for {
		select {
		case rec := <-s.queue:
			if err := s.insert(rec); err != nil {
				log.WithError(err).Warn("usage: insert during drain failed")
			}
		default:
			return
		}
	}
}

func (s *usageStore) retentionLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.applyRetention()
		case <-s.stop:
			s.applyRetention()
			return
		}
	}
}

func (s *usageStore) applyRetention() {
	if s.retentionDays <= 0 {
		return
	}
	cutoff := time.Now().UTC().Add(-time.Duration(s.retentionDays) * 24 * time.Hour)
	_, err := s.db.Exec(`DELETE FROM usage_requests WHERE timestamp < ?`, cutoff)
	if err != nil {
		log.WithError(err).Warn("usage: retention delete requests failed")
	}
	cutoffDay := cutoff.Format("2006-01-02")
	_, err = s.db.Exec(`DELETE FROM usage_daily WHERE day < ?`, cutoffDay)
	if err != nil {
		log.WithError(err).Warn("usage: retention delete daily failed")
	}
}

func (s *usageStore) insert(rec dbRecord) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO usage_requests (
			timestamp, provider, model, credential_label, credential_fingerprint,
			api_key_hash, auth_id, auth_index, source, status_code, failed,
			rate_limited, prompt_tokens, completion_tokens, reasoning_tokens,
			cached_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, rec.Timestamp, rec.Provider, rec.Model, rec.CredentialLabel, rec.CredentialFingerprint,
		rec.APIKeyHash, rec.AuthID, rec.AuthIndex, rec.Source, rec.StatusCode, boolToInt(rec.Failed),
		boolToInt(rec.RateLimited), rec.Tokens.InputTokens, rec.Tokens.OutputTokens, rec.Tokens.ReasoningTokens,
		rec.Tokens.CachedTokens, rec.Tokens.TotalTokens); err != nil {
		return err
	}

	day := rec.Timestamp.Format("2006-01-02")
	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO usage_daily (
			day, provider, credential_fingerprint, credential_label, model,
			total_requests, failed_requests, rate_limited, prompt_tokens,
			completion_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day, provider, credential_fingerprint, model) DO UPDATE SET
			total_requests = usage_daily.total_requests + excluded.total_requests,
			failed_requests = usage_daily.failed_requests + excluded.failed_requests,
			rate_limited = usage_daily.rate_limited + excluded.rate_limited,
			prompt_tokens = usage_daily.prompt_tokens + excluded.prompt_tokens,
			completion_tokens = usage_daily.completion_tokens + excluded.completion_tokens,
			total_tokens = usage_daily.total_tokens + excluded.total_tokens,
			credential_label = CASE
				WHEN excluded.credential_label != '' THEN excluded.credential_label
				ELSE usage_daily.credential_label
			END;
	`, day, rec.Provider, rec.CredentialFingerprint, rec.CredentialLabel, rec.Model,
		1, boolToInt(rec.Failed), boolToInt(rec.RateLimited), rec.Tokens.InputTokens,
		rec.Tokens.OutputTokens, rec.Tokens.TotalTokens); err != nil {
		return err
	}

	return tx.Commit()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *usageStore) close() {
	close(s.stop)
	s.wg.Wait()
	_ = s.db.Close()
}
