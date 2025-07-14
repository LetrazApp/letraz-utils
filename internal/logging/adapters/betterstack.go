package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// BetterstackAdapter implements the LogAdapter interface for Betterstack integration
type BetterstackAdapter struct {
	name          string
	config        BetterstackConfig
	httpClient    *http.Client
	mu            sync.Mutex
	healthy       bool
	lastError     error
	lastErrorTime time.Time
}

// BetterstackConfig represents configuration for the Betterstack adapter
type BetterstackConfig struct {
	SourceToken   string            `yaml:"source_token"`   // Betterstack source token
	Endpoint      string            `yaml:"endpoint"`       // Betterstack API endpoint
	BatchSize     int               `yaml:"batch_size"`     // number of logs to batch together
	FlushInterval time.Duration     `yaml:"flush_interval"` // how often to flush batched logs
	MaxRetries    int               `yaml:"max_retries"`    // max retry attempts
	Timeout       time.Duration     `yaml:"timeout"`        // HTTP request timeout
	UserAgent     string            `yaml:"user_agent"`     // HTTP user agent
	Headers       map[string]string `yaml:"headers"`        // Additional HTTP headers
}

// BetterstackLogEntry represents a log entry in Betterstack format
type BetterstackLogEntry struct {
	Timestamp time.Time              `json:"dt"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// NewBetterstackAdapter creates a new Betterstack adapter
func NewBetterstackAdapter(name string, config BetterstackConfig) (*BetterstackAdapter, error) {
	// Set defaults
	if config.Endpoint == "" {
		config.Endpoint = "https://in.logs.betterstack.com"
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.UserAgent == "" {
		config.UserAgent = "letraz-utils/1.0"
	}
	if config.Headers == nil {
		config.Headers = make(map[string]string)
	}

	// Validate required fields
	if config.SourceToken == "" {
		return nil, fmt.Errorf("source_token is required for Betterstack adapter")
	}

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	adapter := &BetterstackAdapter{
		name:       name,
		config:     config,
		httpClient: httpClient,
		healthy:    true,
	}

	return adapter, nil
}

// Write writes a log entry to Betterstack
func (a *BetterstackAdapter) Write(entry *types.LogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Convert to Betterstack format
	bsEntry := BetterstackLogEntry{
		Timestamp: entry.Timestamp,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    entry.Fields,
	}

	// Send to Betterstack API
	if err := a.sendToBetterstack(bsEntry); err != nil {
		a.healthy = false
		a.lastError = err
		a.lastErrorTime = time.Now()
		return fmt.Errorf("failed to send log to Betterstack: %w", err)
	}

	a.healthy = true
	a.lastError = nil
	return nil
}

// Close closes the adapter
func (a *BetterstackAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Close HTTP client if it has a CloseIdleConnections method
	if transport, ok := a.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// Health returns the health status of the adapter
func (a *BetterstackAdapter) Health() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.healthy {
		return fmt.Errorf("adapter unhealthy: %v (last error at %v)",
			a.lastError, a.lastErrorTime)
	}

	return nil
}

// Name returns the name of the adapter
func (a *BetterstackAdapter) Name() string {
	return a.name
}

// sendToBetterstack sends a log entry to the Betterstack API
func (a *BetterstackAdapter) sendToBetterstack(entry BetterstackLogEntry) error {
	// Create the request payload
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", a.config.Endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.SourceToken)
	req.Header.Set("User-Agent", a.config.UserAgent)

	// Add additional headers
	for key, value := range a.config.Headers {
		req.Header.Set(key, value)
	}

	// Send request with retry logic
	var lastErr error
	for i := 0; i <= a.config.MaxRetries; i++ {
		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if i < a.config.MaxRetries {
				time.Sleep(time.Duration(i+1) * time.Second) // Linear backoff
				continue
			}
			break
		}

		// Handle response
		if err := a.handleResponse(resp); err != nil {
			lastErr = err
			if i < a.config.MaxRetries && a.isRetryableError(resp.StatusCode) {
				time.Sleep(time.Duration(i+1) * time.Second) // Linear backoff
				continue
			}
			break
		}

		// Success
		return nil
	}

	return fmt.Errorf("failed to send log after %d retries: %w", a.config.MaxRetries, lastErr)
}

// handleResponse handles the HTTP response from Betterstack
func (a *BetterstackAdapter) handleResponse(resp *http.Response) error {
	defer resp.Body.Close()

	// Read response body with size limit to prevent memory exhaustion
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // Limit to 1MB
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil // Success
	}

	// Handle error responses
	switch resp.StatusCode {
	case 400:
		return fmt.Errorf("bad request: %s", string(body))
	case 401:
		return fmt.Errorf("unauthorized: invalid source token")
	case 403:
		return fmt.Errorf("forbidden: access denied")
	case 429:
		return fmt.Errorf("rate limited: %s", string(body))
	case 500, 502, 503, 504:
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	default:
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}
}

// isRetryableError determines if an HTTP status code is retryable
func (a *BetterstackAdapter) isRetryableError(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// GetStats returns statistics about the adapter
func (a *BetterstackAdapter) GetStats() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats := map[string]interface{}{
		"healthy":         a.healthy,
		"last_error":      nil,
		"last_error_time": nil,
		"endpoint":        a.config.Endpoint,
		"batch_size":      a.config.BatchSize,
		"flush_interval":  a.config.FlushInterval.String(),
		"max_retries":     a.config.MaxRetries,
		"timeout":         a.config.Timeout.String(),
	}

	if a.lastError != nil {
		stats["last_error"] = a.lastError.Error()
		stats["last_error_time"] = a.lastErrorTime
	}

	return stats
}
