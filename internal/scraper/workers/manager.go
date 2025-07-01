package workers

import (
	"context"
	"fmt"
	"sync"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/scraper"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"

	"github.com/sirupsen/logrus"
)

// PoolManager manages the worker pool lifecycle
type PoolManager struct {
	config         *config.Config
	pool           *WorkerPool
	scraperFactory scraper.ScraperFactory
	logger         *logrus.Logger
	mu             sync.RWMutex
	initialized    bool
}

// NewPoolManager creates a new worker pool manager
func NewPoolManager(cfg *config.Config) *PoolManager {
	return &PoolManager{
		config:         cfg,
		scraperFactory: scraper.NewScraperFactory(cfg),
		logger:         utils.GetLogger().WithField("component", "pool_manager").Logger,
	}
}

// Initialize initializes the worker pool
func (pm *PoolManager) Initialize() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.initialized {
		return fmt.Errorf("worker pool already initialized")
	}

	pm.logger.Info("Initializing worker pool")

	// Create the worker pool
	pm.pool = NewWorkerPool(pm.config, pm.scraperFactory)

	// Start the worker pool
	err := pm.pool.Start()
	if err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	pm.initialized = true
	pm.logger.Info("Worker pool initialized successfully")
	return nil
}

// Shutdown gracefully shuts down the worker pool
func (pm *PoolManager) Shutdown() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.initialized || pm.pool == nil {
		return nil
	}

	pm.logger.Info("Shutting down worker pool")

	err := pm.pool.Stop()
	if err != nil {
		pm.logger.WithError(err).Error("Error stopping worker pool")
		return err
	}

	// Stop rate limiter cleanup
	pm.pool.rateLimiter.Stop()

	pm.initialized = false
	pm.logger.Info("Worker pool shutdown complete")
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
		PoolStats:        poolStats,
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
	PoolStats        PoolStats                         `json:"pool_stats"`
	RateLimiterStats map[string]map[string]interface{} `json:"rate_limiter_stats"`
	WorkerCount      int                               `json:"worker_count"`
	QueueCapacity    int                               `json:"queue_capacity"`
}
