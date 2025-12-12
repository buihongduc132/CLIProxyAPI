package usage

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestUsageStoreInsertAndAggregate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := newUsageStore(DatabaseOptions{
		Enabled:       true,
		Path:          path,
		RetentionDays: 3,
	})
	if err != nil {
		t.Fatalf("failed to create usage store: %v", err)
	}
	defer store.close()

	rec := dbRecord{
		Timestamp:             time.Now().UTC(),
		Provider:              "qwen",
		Model:                 "qwen3-coder",
		CredentialLabel:       "acct@example.com",
		CredentialFingerprint: "fingerprint",
		APIKeyHash:            "hash",
		AuthID:                "auth-A",
		AuthIndex:             7,
		Source:                "source-A",
		StatusCode:            200,
		Failed:                false,
		RateLimited:           false,
		Tokens: TokenStats{
			InputTokens:  10,
			OutputTokens: 15,
			TotalTokens:  25,
		},
	}

	if err := store.insert(rec); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	var requestCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM usage_requests`).Scan(&requestCount); err != nil {
		t.Fatalf("query usage_requests failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected 1 request row, got %d", requestCount)
	}

	var totalRequests, failedRequests, storedTokens int
	if err := store.db.QueryRow(`SELECT total_requests, failed_requests, total_tokens FROM usage_daily WHERE provider = ? AND model = ?`,
		"qwen", "qwen3-coder").Scan(&totalRequests, &failedRequests, &storedTokens); err != nil {
		t.Fatalf("query usage_daily failed: %v", err)
	}
	if totalRequests != 1 || failedRequests != 0 || storedTokens != 25 {
		t.Fatalf("unexpected aggregate: requests=%d failed=%d tokens=%d", totalRequests, failedRequests, storedTokens)
	}
}

func TestDatabasePluginHandleUsage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "usage.db")
	if err := ConfigureDatabase(DatabaseOptions{
		Enabled:       true,
		Path:          path,
		RetentionDays: 2,
	}); err != nil {
		t.Fatalf("configure database failed: %v", err)
	}
	defer ConfigureDatabase(DatabaseOptions{})

	store := currentUsageStore.Load()
	if store == nil {
		t.Fatal("expected usage store to be configured")
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Writer.WriteHeader(429)

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	record := coreusage.Record{
		Provider:    "claude",
		Model:       "claude-3",
		AuthID:      "auth-X",
		AuthIndex:   2,
		Source:      "source-X",
		RequestedAt: time.Now(),
		Failed:      true,
		Detail: coreusage.Detail{
			InputTokens:  4,
			OutputTokens: 6,
		},
	}

	plugin := databasePlugin{}
	plugin.HandleUsage(ctx, record)

	deadline := time.Now().Add(2 * time.Second)
	for {
		var rateLimitedCount int
		err := store.db.QueryRow(`SELECT SUM(rate_limited) FROM usage_daily WHERE provider = ? AND model = ?`,
			"claude", "claude-3").Scan(&rateLimitedCount)
		if err == nil && rateLimitedCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("usage record was not persisted before timeout (err=%v count=%d)", err, rateLimitedCount)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
