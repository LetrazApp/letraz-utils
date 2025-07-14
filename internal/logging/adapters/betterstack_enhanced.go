package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// BetterstackEnhancedAdapter implements the LogAdapter interface with circuit breaker and retry logic
type BetterstackEnhancedAdapter struct {
	name           string
	config         BetterstackEnhancedConfig
	httpClient     *http.Client
	circuitBreaker *CircuitBreaker
	mu             sync.Mutex
	healthy        bool
	lastError      error
	lastErrorTime  time.Time
	stats          *AdapterStats
}

// BetterstackEnhancedConfig represents enhanced configuration for the Betterstack adapter
type BetterstackEnhancedConfig struct {
	SourceToken   string            `yaml:"source_token"`
	Endpoint      string            `yaml:"endpoint"`
	BatchSize     int               `yaml:"batch_size"`
	FlushInterval time.Duration     `yaml:"flush_interval"`
	MaxRetries    int               `yaml:"max_retries"`
	Timeout       time.Duration     `yaml:"timeout"`
	UserAgent     string            `yaml:"user_agent"`
	Headers       map[string]string `yaml:"headers"`

	// Circuit breaker configuration
	CircuitBreaker struct {
		FailureThreshold int           `yaml:"failure_threshold"`
		ResetTimeout     time.Duration `yaml:"reset_timeout"`
		HalfOpenMaxCalls int           `yaml:"half_open_max_calls"`
	} `yaml:"circuit_breaker"`

	// Retry configuration
	Retry struct {
		InitialInterval    time.Duration `yaml:"initial_interval"`
		MaxInterval        time.Duration `yaml:"max_interval"`
		ExponentialBackoff bool          `yaml:"exponential_backoff"`
		Jitter             bool          `yaml:"jitter"`
	} `yaml:"retry"`
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	failureThreshold int
	resetTimeout     time.Duration
	halfOpenMaxCalls int

	mu              sync.RWMutex
	state           CircuitState
	failures        int
	lastFailureTime time.Time
	halfOpenCalls   int
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// AdapterStats tracks statistics for the adapter
type AdapterStats struct {
	mu                  sync.RWMutex
	totalRequests       int64
	successfulRequests  int64
	failedRequests      int64
	circuitBreakerTrips int64
	lastRequestTime     time.Time
	averageResponseTime time.Duration
	responseTimeCount   int64
}

// NewBetterstackEnhancedAdapter creates a new enhanced Betterstack adapter
func NewBetterstackEnhancedAdapter(name string, config BetterstackEnhancedConfig) (*BetterstackEnhancedAdapter, error) {
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

	// Circuit breaker defaults
	if config.CircuitBreaker.FailureThreshold == 0 {
		config.CircuitBreaker.FailureThreshold = 5
	}
	if config.CircuitBreaker.ResetTimeout == 0 {
		config.CircuitBreaker.ResetTimeout = 30 * time.Second
	}
	if config.CircuitBreaker.HalfOpenMaxCalls == 0 {
		config.CircuitBreaker.HalfOpenMaxCalls = 3
	}

	// Retry defaults
	if config.Retry.InitialInterval == 0 {
		config.Retry.InitialInterval = time.Second
	}
	if config.Retry.MaxInterval == 0 {
		config.Retry.MaxInterval = 30 * time.Second
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

	// Create circuit breaker
	circuitBreaker := &CircuitBreaker{
		failureThreshold: config.CircuitBreaker.FailureThreshold,
		resetTimeout:     config.CircuitBreaker.ResetTimeout,
		halfOpenMaxCalls: config.CircuitBreaker.HalfOpenMaxCalls,
		state:            CircuitClosed,
	}

	adapter := &BetterstackEnhancedAdapter{
		name:           name,
		config:         config,
		httpClient:     httpClient,
		circuitBreaker: circuitBreaker,
		healthy:        true,
		stats:          &AdapterStats{},
	}

	return adapter, nil
}

// Write writes a log entry to Betterstack with circuit breaker protection
func (a *BetterstackEnhancedAdapter) Write(entry *types.LogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check circuit breaker
	if !a.circuitBreaker.CanCall() {
		a.stats.recordCircuitBreakerTrip()
		return fmt.Errorf("circuit breaker is open")
	}

	// Convert to Betterstack format
	bsEntry := BetterstackLogEntry{
		Timestamp: entry.Timestamp,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    entry.Fields,
	}

	// Send to Betterstack API with retry logic
	start := time.Now()
	err := a.sendToBetterstackWithRetry(bsEntry)
	duration := time.Since(start)

	a.stats.recordRequest(duration, err == nil)

	if err != nil {
		a.circuitBreaker.RecordFailure()
		a.healthy = false
		a.lastError = err
		a.lastErrorTime = time.Now()
		return fmt.Errorf("failed to send log to Betterstack: %w", err)
	}

	a.circuitBreaker.RecordSuccess()
	a.healthy = true
	a.lastError = nil
	return nil
}

// sendToBetterstackWithRetry sends a log entry with exponential backoff retry
func (a *BetterstackEnhancedAdapter) sendToBetterstackWithRetry(entry BetterstackLogEntry) error {
	var lastErr error
	interval := a.config.Retry.InitialInterval

	for i := 0; i <= a.config.MaxRetries; i++ {
		// Check circuit breaker on each retry
		if !a.circuitBreaker.CanCall() {
			return fmt.Errorf("circuit breaker opened during retry")
		}

		err := a.sendToBetterstack(entry)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Don't retry on certain errors
		if !a.shouldRetry(err) {
			break
		}

		// Don't sleep after the last attempt
		if i < a.config.MaxRetries {
			// Apply jitter if enabled
			sleepDuration := interval
			if a.config.Retry.Jitter {
				sleepDuration = a.addJitter(interval)
			}

			time.Sleep(sleepDuration)

			// Exponential backoff
			if a.config.Retry.ExponentialBackoff {
				interval *= 2
				if interval > a.config.Retry.MaxInterval {
					interval = a.config.Retry.MaxInterval
				}
			}
		}
	}

	return fmt.Errorf("failed to send log after %d retries: %w", a.config.MaxRetries, lastErr)
}

// sendToBetterstack sends a log entry to the Betterstack API
func (a *BetterstackEnhancedAdapter) sendToBetterstack(entry BetterstackLogEntry) error {
	// Create the request payload
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Create HTTP request with context
	ctx, cancel := context.WithTimeout(context.Background(), a.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.Endpoint, bytes.NewBuffer(payload))
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

	// Send request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}

	return a.handleResponse(resp)
}

// handleResponse handles the HTTP response from Betterstack
func (a *BetterstackEnhancedAdapter) handleResponse(resp *http.Response) error {
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
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "bad request", Body: string(body), Retryable: false}
	case 401:
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "unauthorized", Body: string(body), Retryable: false}
	case 403:
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "forbidden", Body: string(body), Retryable: false}
	case 429:
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "rate limited", Body: string(body), Retryable: true}
	case 500, 502, 503, 504:
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "server error", Body: string(body), Retryable: true}
	default:
		return &BetterstackError{StatusCode: resp.StatusCode, Message: "unexpected error", Body: string(body), Retryable: false}
	}
}

// shouldRetry determines if an error should be retried
func (a *BetterstackEnhancedAdapter) shouldRetry(err error) bool {
	if bsErr, ok := err.(*BetterstackError); ok {
		return bsErr.Retryable
	}
	return true // Retry unknown errors
}

// addJitter adds random jitter to the interval
func (a *BetterstackEnhancedAdapter) addJitter(interval time.Duration) time.Duration {
	// Add random jitter between -10% and +10%
	jitterRange := float64(interval) * 0.1
	jitter := time.Duration((rand.Float64()*2 - 1) * jitterRange)
	return interval + jitter
}

// Close closes the adapter
func (a *BetterstackEnhancedAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if transport, ok := a.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// Health returns the health status of the adapter
func (a *BetterstackEnhancedAdapter) Health() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.healthy {
		return fmt.Errorf("adapter unhealthy: %v (last error at %v)", a.lastError, a.lastErrorTime)
	}

	if a.circuitBreaker.GetState() == CircuitOpen {
		return fmt.Errorf("circuit breaker is open")
	}

	return nil
}

// Name returns the name of the adapter
func (a *BetterstackEnhancedAdapter) Name() string {
	return a.name
}

// GetStats returns detailed statistics about the adapter
func (a *BetterstackEnhancedAdapter) GetStats() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats := a.stats.GetStats()
	stats["circuit_breaker_state"] = a.circuitBreaker.GetState().String()
	stats["healthy"] = a.healthy

	if a.lastError != nil {
		stats["last_error"] = a.lastError.Error()
		stats["last_error_time"] = a.lastErrorTime
	}

	return stats
}

// Circuit breaker methods

// CanCall checks if the circuit breaker allows the call
func (cb *CircuitBreaker) CanCall() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.halfOpenCalls = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		if cb.halfOpenCalls < cb.halfOpenMaxCalls {
			cb.halfOpenCalls++
			return true
		}
		return false
	default:
		return false
	}
}

// RecordSuccess records a successful call
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = CircuitClosed
	cb.halfOpenCalls = 0
}

// RecordFailure records a failed call
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.halfOpenCalls = 0
	} else if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
	}
}

// GetState returns the current circuit state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// String returns the string representation of the circuit state
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Statistics methods

// recordRequest records a request and its outcome
func (s *AdapterStats) recordRequest(duration time.Duration, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalRequests++
	s.lastRequestTime = time.Now()

	if success {
		s.successfulRequests++
	} else {
		s.failedRequests++
	}

	// Update average response time
	s.averageResponseTime = time.Duration(
		(int64(s.averageResponseTime)*s.responseTimeCount + int64(duration)) / (s.responseTimeCount + 1),
	)
	s.responseTimeCount++
}

// recordCircuitBreakerTrip records a circuit breaker trip
func (s *AdapterStats) recordCircuitBreakerTrip() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.circuitBreakerTrips++
}

// GetStats returns the current statistics
func (s *AdapterStats) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	successRate := float64(0)
	if s.totalRequests > 0 {
		successRate = float64(s.successfulRequests) / float64(s.totalRequests) * 100
	}

	return map[string]interface{}{
		"total_requests":        s.totalRequests,
		"successful_requests":   s.successfulRequests,
		"failed_requests":       s.failedRequests,
		"success_rate":          successRate,
		"circuit_breaker_trips": s.circuitBreakerTrips,
		"last_request_time":     s.lastRequestTime,
		"average_response_time": s.averageResponseTime.String(),
	}
}

// BetterstackError represents an error from the Betterstack API
type BetterstackError struct {
	StatusCode int
	Message    string
	Body       string
	Retryable  bool
}

// Error implements the error interface
func (e *BetterstackError) Error() string {
	return fmt.Sprintf("Betterstack API error (%d): %s - %s", e.StatusCode, e.Message, e.Body)
}
