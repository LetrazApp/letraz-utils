package logging

import (
	"fmt"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// ErrorHandler handles logging errors and provides fallback mechanisms
type ErrorHandler struct {
	maxRetries      int
	retryDelay      time.Duration
	fallbackAdapter types.LogAdapter
	errorCallback   func(error, string)
	mu              sync.RWMutex
}

// ErrorHandlerConfig configures the error handler
type ErrorHandlerConfig struct {
	MaxRetries      int           `yaml:"max_retries"`
	RetryDelay      time.Duration `yaml:"retry_delay"`
	FallbackAdapter types.LogAdapter
	ErrorCallback   func(error, string)
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(config ErrorHandlerConfig) *ErrorHandler {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}

	return &ErrorHandler{
		maxRetries:      config.MaxRetries,
		retryDelay:      config.RetryDelay,
		fallbackAdapter: config.FallbackAdapter,
		errorCallback:   config.ErrorCallback,
	}
}

// HandleError handles an error from a logging adapter
func (h *ErrorHandler) HandleError(err error, adapterName string, entry *types.LogEntry) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Call error callback if provided
	if h.errorCallback != nil {
		h.errorCallback(err, adapterName)
	}

	// Try fallback adapter if available
	if h.fallbackAdapter != nil {
		if fallbackErr := h.fallbackAdapter.Write(entry); fallbackErr != nil {
			// Log to stderr as last resort
			fmt.Printf("ERROR: Fallback adapter failed for %s: %v (original error: %v)\n",
				adapterName, fallbackErr, err)
		}
	}
}

// RetryWithBackoff retries an operation with exponential backoff
func (h *ErrorHandler) RetryWithBackoff(operation func() error) error {
	var lastErr error
	delay := h.retryDelay

	for i := 0; i < h.maxRetries; i++ {
		if err := operation(); err != nil {
			lastErr = err
			if i < h.maxRetries-1 {
				time.Sleep(delay)
				delay *= 2 // Exponential backoff
			}
		} else {
			return nil
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", h.maxRetries, lastErr)
}

// LoggingError represents different types of logging errors
type LoggingError struct {
	Type      ErrorType
	Message   string
	Adapter   string
	Timestamp time.Time
	Cause     error
}

// ErrorType represents the type of logging error
type ErrorType int

const (
	ErrorTypeAdapterFailure ErrorType = iota
	ErrorTypeConfiguration
	ErrorTypeFormatter
	ErrorTypeRotation
	ErrorTypeNetwork
	ErrorTypePermission
	ErrorTypeCapacity
)

// String returns the string representation of the error type
func (e ErrorType) String() string {
	switch e {
	case ErrorTypeAdapterFailure:
		return "adapter_failure"
	case ErrorTypeConfiguration:
		return "configuration"
	case ErrorTypeFormatter:
		return "formatter"
	case ErrorTypeRotation:
		return "rotation"
	case ErrorTypeNetwork:
		return "network"
	case ErrorTypePermission:
		return "permission"
	case ErrorTypeCapacity:
		return "capacity"
	default:
		return "unknown"
	}
}

// Error implements the error interface
func (e *LoggingError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("logging error [%s] in adapter %s: %s (caused by: %v)",
			e.Type.String(), e.Adapter, e.Message, e.Cause)
	}
	return fmt.Sprintf("logging error [%s] in adapter %s: %s",
		e.Type.String(), e.Adapter, e.Message)
}

// NewLoggingError creates a new logging error
func NewLoggingError(errorType ErrorType, adapter, message string, cause error) *LoggingError {
	return &LoggingError{
		Type:      errorType,
		Message:   message,
		Adapter:   adapter,
		Timestamp: time.Now(),
		Cause:     cause,
	}
}

// ErrorRecovery provides recovery mechanisms for logging failures
type ErrorRecovery struct {
	circuitBreaker *CircuitBreaker
	healthChecker  *HealthChecker
}

// CircuitBreaker implements the circuit breaker pattern for logging adapters
type CircuitBreaker struct {
	failureThreshold int
	resetTimeout     time.Duration
	halfOpenMaxCalls int
	failures         int
	lastFailureTime  time.Time
	halfOpenCalls    int
	state            CircuitState
	mu               sync.RWMutex
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

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

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
		halfOpenMaxCalls: 3, // Default to 3 test calls in half-open state
		state:            CircuitClosed,
	}
}

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

// HealthChecker monitors the health of logging adapters
type HealthChecker struct {
	adapters      map[string]types.LogAdapter
	checkInterval time.Duration
	mu            sync.RWMutex
	stopCh        chan struct{}
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(checkInterval time.Duration) *HealthChecker {
	return &HealthChecker{
		adapters:      make(map[string]types.LogAdapter),
		checkInterval: checkInterval,
		stopCh:        make(chan struct{}),
	}
}

// AddAdapter adds an adapter to health monitoring
func (hc *HealthChecker) AddAdapter(adapter types.LogAdapter) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.adapters[adapter.Name()] = adapter
}

// RemoveAdapter removes an adapter from health monitoring
func (hc *HealthChecker) RemoveAdapter(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	delete(hc.adapters, name)
}

// Start starts the health checker
func (hc *HealthChecker) Start() {
	go hc.run()
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

// run runs the health check loop
func (hc *HealthChecker) run() {
	ticker := time.NewTicker(hc.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.checkHealth()
		case <-hc.stopCh:
			return
		}
	}
}

// checkHealth checks the health of all adapters
func (hc *HealthChecker) checkHealth() {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	for name, adapter := range hc.adapters {
		if err := adapter.Health(); err != nil {
			fmt.Printf("WARN: Health check failed for adapter %s: %v\n", name, err)
		}
	}
}
