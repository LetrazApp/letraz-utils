package workers

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"

	"golang.org/x/time/rate"
)

// DomainLimiter represents rate limiting for a specific domain
type DomainLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	requests int64
	failures int64
	mu       sync.RWMutex
}

// CircuitBreaker represents a circuit breaker for a domain
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	failureCount int
	lastFailTime time.Time
	state        CircuitState
	mu           sync.RWMutex
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// RateLimiter manages rate limiting and circuit breaking per domain
type RateLimiter struct {
	config          *config.Config
	domainLimiters  map[string]*DomainLimiter
	circuitBreakers map[string]*CircuitBreaker
	mu              sync.RWMutex
	logger          logging.Logger
	cleanupTicker   *time.Ticker
	stopCleanup     chan bool
}

// NewRateLimiter creates a new rate limiter instance
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	rl := &RateLimiter{
		config:          cfg,
		domainLimiters:  make(map[string]*DomainLimiter),
		circuitBreakers: make(map[string]*CircuitBreaker),
		logger:          logging.GetGlobalLogger(),
		cleanupTicker:   time.NewTicker(5 * time.Minute),
		stopCleanup:     make(chan bool),
	}

	// Start cleanup goroutine
	go rl.cleanupRoutine()

	return rl
}

// Allow checks if a request to the given domain is allowed
func (rl *RateLimiter) Allow(domain string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Normalize domain
	domain = strings.ToLower(domain)

	// Check circuit breaker first
	if !rl.isCircuitClosed(domain) {
		rl.logger.Debug("Request rejected by circuit breaker", map[string]interface{}{
			"domain": domain,
		})
		return false
	}

	// Get or create domain limiter
	limiter := rl.getDomainLimiter(domain)

	// Check rate limit
	allowed := limiter.limiter.Allow()
	if allowed {
		limiter.mu.Lock()
		limiter.requests++
		limiter.lastSeen = time.Now()
		limiter.mu.Unlock()

		rl.logger.Debug("Request allowed", map[string]interface{}{
			"domain":   domain,
			"requests": limiter.requests,
		})
	} else {
		rl.logger.Debug("Request rejected by rate limiter", map[string]interface{}{
			"domain": domain,
		})
	}

	return allowed
}

// RecordSuccess records a successful request for the domain
func (rl *RateLimiter) RecordSuccess(domain string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	domain = strings.ToLower(domain)

	// Reset circuit breaker failure count on success
	if cb, exists := rl.circuitBreakers[domain]; exists {
		cb.mu.Lock()
		if cb.state == CircuitHalfOpen {
			cb.state = CircuitClosed
			cb.failureCount = 0
			rl.logger.Info("Circuit breaker closed after successful request", map[string]interface{}{
				"domain": domain,
			})
		}
		cb.mu.Unlock()
	}
}

// RecordFailure records a failed request for the domain
func (rl *RateLimiter) RecordFailure(domain string, err error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	domain = strings.ToLower(domain)

	// Update domain limiter failure count
	if limiter, exists := rl.domainLimiters[domain]; exists {
		limiter.mu.Lock()
		limiter.failures++
		limiter.mu.Unlock()
	}

	// Update circuit breaker
	cb := rl.getCircuitBreaker(domain)
	cb.mu.Lock()
	cb.failureCount++
	cb.lastFailTime = time.Now()

	if cb.failureCount >= cb.maxFailures && cb.state == CircuitClosed {
		cb.state = CircuitOpen
		rl.logger.Warn("Circuit breaker opened due to failures", map[string]interface{}{
			"domain":   domain,
			"failures": cb.failureCount,
			"error":    err.Error(),
		})
	}
	cb.mu.Unlock()
}

// getDomainLimiter gets or creates a rate limiter for a domain
func (rl *RateLimiter) getDomainLimiter(domain string) *DomainLimiter {
	if limiter, exists := rl.domainLimiters[domain]; exists {
		return limiter
	}

	// Create new limiter
	// Rate limit: requests per minute converted to requests per second
	rps := rate.Limit(float64(rl.config.Workers.RateLimit) / 60.0)
	burst := 5 // Allow bursts of up to 5 requests

	limiter := &DomainLimiter{
		limiter:  rate.NewLimiter(rps, burst),
		lastSeen: time.Now(),
	}

	rl.domainLimiters[domain] = limiter

	rl.logger.Info("Created new domain rate limiter", map[string]interface{}{
		"domain": domain,
		"rate":   rps,
		"burst":  burst,
	})

	return limiter
}

// getCircuitBreaker gets or creates a circuit breaker for a domain
func (rl *RateLimiter) getCircuitBreaker(domain string) *CircuitBreaker {
	if cb, exists := rl.circuitBreakers[domain]; exists {
		return cb
	}

	cb := &CircuitBreaker{
		maxFailures:  5,                // Open circuit after 5 failures
		resetTimeout: 30 * time.Second, // Try to close after 30 seconds
		state:        CircuitClosed,
	}

	rl.circuitBreakers[domain] = cb

	rl.logger.Info("Created new circuit breaker", map[string]interface{}{
		"domain": domain,
	})

	return cb
}

// isCircuitClosed checks if the circuit breaker allows requests
func (rl *RateLimiter) isCircuitClosed(domain string) bool {
	cb := rl.getCircuitBreaker(domain)

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailTime) > cb.resetTimeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			if cb.state == CircuitOpen && time.Since(cb.lastFailTime) > cb.resetTimeout {
				cb.state = CircuitHalfOpen
				rl.logger.Info("Circuit breaker transitioned to half-open", map[string]interface{}{
					"domain": domain,
				})
			}
			cb.mu.Unlock()
			cb.mu.RLock()
			return cb.state == CircuitHalfOpen
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

// GetDomainStats returns statistics for a specific domain
func (rl *RateLimiter) GetDomainStats(domain string) map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	domain = strings.ToLower(domain)
	stats := make(map[string]interface{})

	// Rate limiter stats
	if limiter, exists := rl.domainLimiters[domain]; exists {
		limiter.mu.RLock()
		stats["requests"] = limiter.requests
		stats["failures"] = limiter.failures
		stats["last_seen"] = limiter.lastSeen
		stats["limit"] = limiter.limiter.Limit()
		stats["burst"] = limiter.limiter.Burst()
		limiter.mu.RUnlock()
	}

	// Circuit breaker stats
	if cb, exists := rl.circuitBreakers[domain]; exists {
		cb.mu.RLock()
		stats["circuit_state"] = cb.state.String()
		stats["failure_count"] = cb.failureCount
		stats["max_failures"] = cb.maxFailures
		stats["last_fail_time"] = cb.lastFailTime
		cb.mu.RUnlock()
	}

	return stats
}

// GetAllStats returns statistics for all domains
func (rl *RateLimiter) GetAllStats() map[string]map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	allStats := make(map[string]map[string]interface{})

	// Get all unique domains
	domains := make(map[string]bool)
	for domain := range rl.domainLimiters {
		domains[domain] = true
	}
	for domain := range rl.circuitBreakers {
		domains[domain] = true
	}

	// Get stats for each domain
	for domain := range domains {
		allStats[domain] = rl.GetDomainStats(domain)
	}

	return allStats
}

// cleanupRoutine periodically cleans up old unused limiters
func (rl *RateLimiter) cleanupRoutine() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			rl.cleanupTicker.Stop()
			return
		}
	}
}

// cleanup removes old unused limiters and circuit breakers
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	removedCount := 0

	// Clean up domain limiters
	for domain, limiter := range rl.domainLimiters {
		limiter.mu.RLock()
		lastSeen := limiter.lastSeen
		limiter.mu.RUnlock()

		if lastSeen.Before(cutoff) {
			delete(rl.domainLimiters, domain)
			removedCount++
		}
	}

	// Clean up circuit breakers that haven't seen failures recently
	for domain, cb := range rl.circuitBreakers {
		cb.mu.RLock()
		lastFailTime := cb.lastFailTime
		state := cb.state
		cb.mu.RUnlock()

		if state == CircuitClosed && lastFailTime.Before(cutoff) {
			delete(rl.circuitBreakers, domain)
		}
	}

	if removedCount > 0 {
		rl.logger.Info("Cleaned up unused rate limiters", map[string]interface{}{
			"removed_count": removedCount,
		})
	}
}

// Stop stops the rate limiter and cleanup routine
func (rl *RateLimiter) Stop() {
	rl.stopCleanup <- true
}

// String returns string representation of CircuitState
func (cs CircuitState) String() string {
	switch cs {
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

// extractDomainFromURL extracts the domain from a URL string
func extractDomainFromURL(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// Fallback to simple extraction if URL parsing fails
		return "unknown"
	}

	domain := parsedURL.Hostname()
	if domain == "" {
		return "unknown"
	}

	return strings.ToLower(domain)
}
