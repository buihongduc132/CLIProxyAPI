// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// OTLPPlugin sends usage records to an OTLP endpoint for collection by dy-noti
type OTLPPlugin struct {
	endpoint    string
	client      *http.Client
	enabled     bool
	enabledMu   sync.RWMutex
	batch       []coreusage.Record
	batchMu     sync.Mutex
	batchSize   int
	batchTimer  *time.Timer
	flushTicker *time.Ticker
	stopChan    chan struct{}
}

// OTLPEvent represents the structure of an event sent to OTLP
type OTLPEvent struct {
	Component         string                 `json:"component"`
	Event             string                 `json:"event"`
	Timestamp         string                 `json:"ts"`
	Provider          string                 `json:"provider"`
	Model             string                 `json:"model"`
	AccountEmail      string                 `json:"account_email,omitempty"`
	ConversationID    string                 `json:"conversation_id,omitempty"`
	TurnID            string                 `json:"turn_id,omitempty"`
	Tokens            map[string]int64       `json:"tokens,omitempty"`
	RequestDurationMs int64                  `json:"request_duration_ms,omitempty"`
	StatusCode        int                    `json:"status_code,omitempty"`
	Attributes        map[string]interface{} `json:"attributes,omitempty"`
}

// NewOTLPPlugin creates a new OTLP plugin with default configuration
func NewOTLPPlugin() *OTLPPlugin {
	endpoint := os.Getenv("DY_NOTI_OTEL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:4318/v1/logs"
	}

	plugin := &OTLPPlugin{
		endpoint:  endpoint,
		client:    &http.Client{Timeout: 5 * time.Second},
		enabled:   true,
		batchSize: 10,
		batch:     make([]coreusage.Record, 0, 10),
		stopChan:  make(chan struct{}),
	}

	// Start periodic batch flush
	plugin.flushTicker = time.NewTicker(5 * time.Second)
	go plugin.periodicFlush()

	return plugin
}

// HandleUsage implements coreusage.Plugin interface
func (p *OTLPPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	p.enabledMu.RLock()
	enabled := p.enabled
	p.enabledMu.RUnlock()

	if !enabled {
		return
	}

	// Convert the usage record to an OTLP event
	event := p.convertRecordToEvent(ctx, record)

	// Send the event immediately (for now, later we'll batch)
	if err := p.sendEvent(event); err != nil {
		log.Errorf("OTLP plugin: failed to send event: %v", err)
	}
}

// convertRecordToEvent converts a usage record to an OTLP event
func (p *OTLPPlugin) convertRecordToEvent(ctx context.Context, record coreusage.Record) *OTLPEvent {
	event := &OTLPEvent{
		Component: "cli-proxy-api",
		Event:     "usage.record",
		Timestamp: record.RequestedAt.Format(time.RFC3339Nano),
		Provider:  record.Provider,
		Model:     record.Model,
		Tokens: map[string]int64{
			"input":     record.Detail.InputTokens,
			"output":    record.Detail.OutputTokens,
			"reasoning": record.Detail.ReasoningTokens,
			"cached":    record.Detail.CachedTokens,
			"total":     record.Detail.TotalTokens,
		},
		StatusCode: 200, // Default, will be overridden if needed
		Attributes: map[string]interface{}{
			"api_key":    record.APIKey,
			"auth_id":    record.AuthID,
			"auth_index": record.AuthIndex,
			"source":     record.Source,
			"failed":     record.Failed,
		},
	}

	// Extract account information from context if available
	if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil {
		// Try to get account info from auth manager if available
		if authValue, exists := ginCtx.Get("auth_value"); exists {
			if authStr, ok := authValue.(string); ok && authStr != "" {
				event.AccountEmail = authStr
			}
		}

		// Extract conversation and turn IDs if available
		if convID, exists := ginCtx.Get("conversation_id"); exists {
			if convStr, ok := convID.(string); ok {
				event.ConversationID = convStr
			}
		}

		if turnID, exists := ginCtx.Get("turn_id"); exists {
			if turnStr, ok := turnID.(string); ok {
				event.TurnID = turnStr
			}
		}

		// Extract status code from response
		if ginCtx.Writer != nil {
			event.StatusCode = ginCtx.Writer.Status()
		}
	}

	return event
}

// sendEvent sends a single event to the OTLP endpoint
func (p *OTLPPlugin) sendEvent(event *OTLPEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", p.endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI-OTLP-Exporter/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return nil
}

// SetEnabled enables or disables the OTLP plugin
func (p *OTLPPlugin) SetEnabled(enabled bool) {
	p.enabledMu.Lock()
	p.enabled = enabled
	p.enabledMu.Unlock()
}

// IsEnabled returns whether the OTLP plugin is enabled
func (p *OTLPPlugin) IsEnabled() bool {
	p.enabledMu.RLock()
	defer p.enabledMu.RUnlock()
	return p.enabled
}

// SetEndpoint sets the OTLP endpoint
func (p *OTLPPlugin) SetEndpoint(endpoint string) {
	p.enabledMu.Lock()
	p.endpoint = strings.TrimSpace(endpoint)
	p.enabledMu.Unlock()
}

// GetEndpoint returns the current OTLP endpoint
func (p *OTLPPlugin) GetEndpoint() string {
	p.enabledMu.RLock()
	defer p.enabledMu.RUnlock()
	return p.endpoint
}

// periodicFlush periodically flushes the batch
func (p *OTLPPlugin) periodicFlush() {
	for {
		select {
		case <-p.stopChan:
			return
		case <-p.flushTicker.C:
			p.flushBatch()
		}
	}
}

// flushBatch sends all accumulated events in the batch
func (p *OTLPPlugin) flushBatch() {
	p.batchMu.Lock()
	if len(p.batch) == 0 {
		p.batchMu.Unlock()
		return
	}

	// Copy the batch and clear it
	batchCopy := make([]coreusage.Record, len(p.batch))
	copy(batchCopy, p.batch)
	p.batch = make([]coreusage.Record, 0, p.batchSize)
	p.batchMu.Unlock()

	// Send each event in the batch
	for _, record := range batchCopy {
		ctx := context.Background() // Use background context for batch sending
		event := p.convertRecordToEvent(ctx, record)
		if err := p.sendEvent(event); err != nil {
			log.Errorf("OTLP plugin: failed to send batched event: %v", err)
		}
	}
}

// Close stops the plugin and flushes any remaining events
func (p *OTLPPlugin) Close() {
	if p.flushTicker != nil {
		p.flushTicker.Stop()
	}

	close(p.stopChan)

	// Flush any remaining events
	p.flushBatch()
}

// Global OTLP plugin instance
var globalOTLPPlugin *OTLPPlugin

// RegisterOTLPPlugin registers the OTLP plugin with the default usage manager
func RegisterOTLPPlugin() {
	plugin := NewOTLPPlugin()
	globalOTLPPlugin = plugin
	coreusage.RegisterPlugin(plugin)
	log.Info("OTLP plugin registered and enabled")
}

// OTLPEnabled returns whether the OTLP plugin is enabled
func OTLPEnabled() bool {
	if globalOTLPPlugin != nil {
		return globalOTLPPlugin.IsEnabled()
	}
	return true // Default to enabled if plugin not initialized
}

// SetOTLPEnabled sets whether the OTLP plugin is enabled
func SetOTLPEnabled(enabled bool) {
	if globalOTLPPlugin != nil {
		globalOTLPPlugin.SetEnabled(enabled)
	}
}

// OTLPEndpoint returns the current OTLP endpoint
func OTLPEndpoint() string {
	if globalOTLPPlugin != nil {
		return globalOTLPPlugin.GetEndpoint()
	}
	return "http://127.0.0.1:4318/v1/logs" // Default endpoint
}

// SetOTLPEndpoint sets the OTLP endpoint
func SetOTLPEndpoint(endpoint string) {
	if globalOTLPPlugin != nil {
		globalOTLPPlugin.SetEndpoint(endpoint)
	}
}
