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

// BetterstackBatchedAdapter implements batched logging to Betterstack
type BetterstackBatchedAdapter struct {
	name           string
	config         BetterstackBatchedConfig
	httpClient     *http.Client
	circuitBreaker *CircuitBreaker
	buffer         *LogBuffer
	flushTimer     *time.Timer
	mu             sync.Mutex
	healthy        bool
	lastError      error
	lastErrorTime  time.Time
	stats          *BatchedAdapterStats
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// BetterstackBatchedConfig represents configuration for the batched Betterstack adapter
type BetterstackBatchedConfig struct {
	SourceToken   string            `yaml:"source_token"`
	Endpoint      string            `yaml:"endpoint"`
	BatchSize     int               `yaml:"batch_size"`
	FlushInterval time.Duration     `yaml:"flush_interval"`
	MaxRetries    int               `yaml:"max_retries"`
	Timeout       time.Duration     `yaml:"timeout"`
	UserAgent     string            `yaml:"user_agent"`
	Headers       map[string]string `yaml:"headers"`

	// Buffer configuration
	Buffer struct {
		MaxSize      int            `yaml:"max_size"`       // Maximum buffer size
		FlushOnLevel types.LogLevel `yaml:"flush_on_level"` // Flush immediately on this level
		CompressLogs bool           `yaml:"compress_logs"`  // Compress batched logs
	} `yaml:"buffer"`

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

// LogBuffer manages batched log entries
type LogBuffer struct {
	entries      []BetterstackLogEntry
	maxSize      int
	flushOnLevel types.LogLevel
	mu           sync.Mutex
}

// BatchedAdapterStats tracks statistics for the batched adapter
type BatchedAdapterStats struct {
	mu                  sync.RWMutex
	totalRequests       int64
	successfulRequests  int64
	failedRequests      int64
	circuitBreakerTrips int64
	totalBatches        int64
	averageBatchSize    float64
	lastBatchTime       time.Time
	bufferOverflows     int64
	immediateFlushes    int64
	timerFlushes        int64
	averageResponseTime time.Duration
	responseTimeCount   int64
}

// NewBetterstackBatchedAdapter creates a new batched Betterstack adapter
func NewBetterstackBatchedAdapter(name string, config BetterstackBatchedConfig) (*BetterstackBatchedAdapter, error) {
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

	// Buffer defaults
	if config.Buffer.MaxSize == 0 {
		config.Buffer.MaxSize = config.BatchSize * 2 // Double the batch size
	}
	if config.Buffer.FlushOnLevel == 0 {
		config.Buffer.FlushOnLevel = types.ErrorLevel // Flush immediately on errors
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

	// Create buffer
	buffer := &LogBuffer{
		entries:      make([]BetterstackLogEntry, 0, config.BatchSize),
		maxSize:      config.Buffer.MaxSize,
		flushOnLevel: config.Buffer.FlushOnLevel,
	}

	adapter := &BetterstackBatchedAdapter{
		name:           name,
		config:         config,
		httpClient:     httpClient,
		circuitBreaker: circuitBreaker,
		buffer:         buffer,
		healthy:        true,
		stats:          &BatchedAdapterStats{},
		stopCh:         make(chan struct{}),
	}

	// Start flush timer
	adapter.resetFlushTimer()

	return adapter, nil
}

// Write writes a log entry to the buffer
func (a *BetterstackBatchedAdapter) Write(entry *types.LogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Convert to Betterstack format
	bsEntry := BetterstackLogEntry{
		Timestamp: entry.Timestamp,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    entry.Fields,
	}

	// Add to buffer
	needsFlush := a.buffer.Add(bsEntry)

	// Check if immediate flush is needed
	if needsFlush || entry.Level >= a.config.Buffer.FlushOnLevel {
		if entry.Level >= a.config.Buffer.FlushOnLevel {
			a.stats.recordImmediateFlush()
		}
		return a.flushBuffer()
	}

	return nil
}

// flushBuffer flushes the current buffer to Betterstack
func (a *BetterstackBatchedAdapter) flushBuffer() error {
	entries := a.buffer.Flush()
	if len(entries) == 0 {
		return nil
	}

	// Check circuit breaker
	if !a.circuitBreaker.CanCall() {
		a.stats.recordCircuitBreakerTrip()
		// Add entries back to buffer if circuit breaker is open
		a.buffer.AddMultiple(entries)
		return fmt.Errorf("circuit breaker is open")
	}

	// Send batch to Betterstack
	start := time.Now()
	err := a.sendBatchToBetterstack(entries)
	duration := time.Since(start)

	a.stats.recordBatch(len(entries), duration, err == nil)

	if err != nil {
		a.circuitBreaker.RecordFailure()
		a.healthy = false
		a.lastError = err
		a.lastErrorTime = time.Now()

		// Add entries back to buffer for retry
		a.buffer.AddMultiple(entries)

		return fmt.Errorf("failed to send batch to Betterstack: %w", err)
	}

	a.circuitBreaker.RecordSuccess()
	a.healthy = true
	a.lastError = nil
	return nil
}

// sendBatchToBetterstack sends a batch of log entries to Betterstack
func (a *BetterstackBatchedAdapter) sendBatchToBetterstack(entries []BetterstackLogEntry) error {
	// Create the request payload
	payload, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	// Compress payload if configured
	if a.config.Buffer.CompressLogs {
		// Compression implementation would go here
		// For now, we'll skip compression
	}

	// Send with retry logic
	return a.sendWithRetry(payload)
}

// sendWithRetry sends a payload with retry logic
func (a *BetterstackBatchedAdapter) sendWithRetry(payload []byte) error {
	var lastErr error
	interval := a.config.Retry.InitialInterval

	for i := 0; i <= a.config.MaxRetries; i++ {
		// Check circuit breaker on each retry
		if !a.circuitBreaker.CanCall() {
			return fmt.Errorf("circuit breaker opened during retry")
		}

		err := a.sendPayload(payload)
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

	return fmt.Errorf("failed to send batch after %d retries: %w", a.config.MaxRetries, lastErr)
}

// sendPayload sends a payload to Betterstack
func (a *BetterstackBatchedAdapter) sendPayload(payload []byte) error {
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
func (a *BetterstackBatchedAdapter) handleResponse(resp *http.Response) error {
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
func (a *BetterstackBatchedAdapter) shouldRetry(err error) bool {
	if bsErr, ok := err.(*BetterstackError); ok {
		return bsErr.Retryable
	}
	return true // Retry unknown errors
}

// addJitter adds random jitter to the interval
func (a *BetterstackBatchedAdapter) addJitter(interval time.Duration) time.Duration {
	// Add random jitter between -10% and +10%
	jitterRange := float64(interval) * 0.1
	jitter := time.Duration((rand.Float64()*2 - 1) * jitterRange)
	return interval + jitter
}

// resetFlushTimer resets the flush timer
func (a *BetterstackBatchedAdapter) resetFlushTimer() {
	if a.flushTimer != nil {
		a.flushTimer.Stop()
	}
	a.flushTimer = time.AfterFunc(a.config.FlushInterval, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.stats.recordTimerFlush()
		a.flushBuffer()
		a.resetFlushTimer()
	})
}

// Flush manually flushes the buffer
func (a *BetterstackBatchedAdapter) Flush() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flushBuffer()
}

// Close closes the adapter
func (a *BetterstackBatchedAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Stop flush timer
	if a.flushTimer != nil {
		a.flushTimer.Stop()
	}

	// Signal shutdown
	close(a.stopCh)

	// Flush remaining entries
	a.flushBuffer()

	// Close HTTP client
	if transport, ok := a.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// Health returns the health status of the adapter
func (a *BetterstackBatchedAdapter) Health() error {
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
func (a *BetterstackBatchedAdapter) Name() string {
	return a.name
}

// GetStats returns detailed statistics about the adapter
func (a *BetterstackBatchedAdapter) GetStats() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats := a.stats.GetStats()
	stats["circuit_breaker_state"] = a.circuitBreaker.GetState().String()
	stats["healthy"] = a.healthy
	stats["buffer_size"] = len(a.buffer.entries)
	stats["buffer_capacity"] = a.buffer.maxSize

	if a.lastError != nil {
		stats["last_error"] = a.lastError.Error()
		stats["last_error_time"] = a.lastErrorTime
	}

	return stats
}

// Buffer methods

// Add adds an entry to the buffer
func (lb *LogBuffer) Add(entry BetterstackLogEntry) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.entries = append(lb.entries, entry)
	return len(lb.entries) >= lb.maxSize
}

// AddMultiple adds multiple entries to the buffer
func (lb *LogBuffer) AddMultiple(entries []BetterstackLogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.entries = append(lb.entries, entries...)
}

// Flush flushes the buffer and returns the entries
func (lb *LogBuffer) Flush() []BetterstackLogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if len(lb.entries) == 0 {
		return nil
	}

	entries := make([]BetterstackLogEntry, len(lb.entries))
	copy(entries, lb.entries)
	lb.entries = lb.entries[:0] // Clear buffer
	return entries
}

// Statistics methods

// recordBatch records a batch operation
func (s *BatchedAdapterStats) recordBatch(batchSize int, duration time.Duration, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalBatches++
	s.lastBatchTime = time.Now()

	// Update average batch size
	s.averageBatchSize = (s.averageBatchSize*float64(s.totalBatches-1) + float64(batchSize)) / float64(s.totalBatches)

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

	s.totalRequests++
}

// recordCircuitBreakerTrip records a circuit breaker trip
func (s *BatchedAdapterStats) recordCircuitBreakerTrip() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.circuitBreakerTrips++
}

// recordImmediateFlush records an immediate flush
func (s *BatchedAdapterStats) recordImmediateFlush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.immediateFlushes++
}

// recordTimerFlush records a timer-based flush
func (s *BatchedAdapterStats) recordTimerFlush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timerFlushes++
}

// GetStats returns the current statistics
func (s *BatchedAdapterStats) GetStats() map[string]interface{} {
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
		"total_batches":         s.totalBatches,
		"average_batch_size":    s.averageBatchSize,
		"circuit_breaker_trips": s.circuitBreakerTrips,
		"buffer_overflows":      s.bufferOverflows,
		"immediate_flushes":     s.immediateFlushes,
		"timer_flushes":         s.timerFlushes,
		"last_batch_time":       s.lastBatchTime,
		"average_response_time": s.averageResponseTime.String(),
	}
}
