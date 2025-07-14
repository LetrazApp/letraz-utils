package workers

import (
	"context"
	"fmt"
	"sync"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper"
	"letraz-utils/pkg/models"
)

// PoolManager manages the worker pool lifecycle
type PoolManager struct {
	config         *config.Config
	pool           *WorkerPool
	scraperFactory scraper.ScraperFactory
	llmManager     *llm.Manager
	logger         logging.Logger
	mu             sync.RWMutex
	initialized    bool
}

// NewPoolManager creates a new worker pool manager
func NewPoolManager(cfg *config.Config, llmManager *llm.Manager) *PoolManager {
	return &PoolManager{
		config:         cfg,
		scraperFactory: scraper.NewScraperFactory(cfg, llmManager),
		llmManager:     llmManager,
		logger:         logging.GetGlobalLogger(),
	}
}

// Initialize initializes the worker pool
func (pm *PoolManager) Initialize() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.initialized {
		return fmt.Errorf("worker pool already initialized")
	}

	pm.logger.Info("Initializing worker pool", nil)
	pm.logger.Debug("DEBUG: PoolManager.Initialize() started", nil)

	// Create the worker pool
	pm.logger.Debug("DEBUG: About to create worker pool", nil)
	pm.pool = NewWorkerPool(pm.config, pm.scraperFactory)
	pm.logger.Debug("DEBUG: Worker pool created successfully", nil)

	// Start the worker pool
	pm.logger.Debug("DEBUG: About to start worker pool", nil)
	err := pm.pool.Start()
	if err != nil {
		pm.logger.Error("DEBUG: Worker pool start failed", map[string]interface{}{
			"error": err.Error(),
		})
		return fmt.Errorf("failed to start worker pool: %w", err)
	}
	pm.logger.Debug("DEBUG: Worker pool start returned successfully", nil)

	pm.initialized = true
	pm.logger.Info("Worker pool initialized successfully", nil)
	pm.logger.Debug("DEBUG: PoolManager.Initialize() completed", nil)
	return nil
}

// Shutdown gracefully shuts down the worker pool
func (pm *PoolManager) Shutdown() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.initialized || pm.pool == nil {
		return nil
	}

	pm.logger.Info("Shutting down worker pool", nil)

	err := pm.pool.Stop()
	if err != nil {
		pm.logger.Error("Error stopping worker pool", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	// Stop rate limiter cleanup
	pm.pool.rateLimiter.Stop()

	pm.initialized = false
	pm.logger.Info("Worker pool shutdown complete", nil)
	return nil
}

// SubmitJob submits a scraping job to the worker pool
func (pm *PoolManager) SubmitJob(ctx context.Context, url string, options *models.ScrapeOptions) (*JobResult, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.initialized || pm.pool == nil {
		return nil, fmt.Errorf("worker pool not initialized")
	}

	return pm.pool.SubmitJob(ctx, url, options)
}

// GetStats returns worker pool statistics
func (pm *PoolManager) GetStats() (*PoolManagerStats, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.initialized || pm.pool == nil {
		return nil, fmt.Errorf("worker pool not initialized")
	}

	poolStats := pm.pool.GetStats()
	rateLimiterStats := pm.pool.rateLimiter.GetAllStats()

	return &PoolManagerStats{
		Initialized:      pm.initialized,
		PoolStats:        &poolStats,
		RateLimiterStats: rateLimiterStats,
		WorkerCount:      len(pm.pool.workers),
		QueueCapacity:    pm.config.Workers.QueueSize,
	}, nil
}

// IsHealthy returns true if the worker pool is healthy
func (pm *PoolManager) IsHealthy() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.initialized && pm.pool != nil && pm.pool.IsRunning()
}

// GetDomainStats returns statistics for a specific domain
func (pm *PoolManager) GetDomainStats(domain string) (map[string]interface{}, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.initialized || pm.pool == nil {
		return nil, fmt.Errorf("worker pool not initialized")
	}

	return pm.pool.rateLimiter.GetDomainStats(domain), nil
}

// PoolManagerStats represents comprehensive statistics for the pool manager
type PoolManagerStats struct {
	Initialized      bool                              `json:"initialized"`
	PoolStats        *PoolStatsData                    `json:"pool_stats"`
	RateLimiterStats map[string]map[string]interface{} `json:"rate_limiter_stats"`
	WorkerCount      int                               `json:"worker_count"`
	QueueCapacity    int                               `json:"queue_capacity"`
}
