package workers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/scraper"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"

	"github.com/sirupsen/logrus"
)

// JobResult represents the result of a scraping job
type JobResult struct {
	Job       *models.JobPosting
	Error     error
	RequestID string
	Duration  time.Duration
}

// ScrapeJob represents a job to be processed by workers
type ScrapeJob struct {
	ID         string
	URL        string
	Options    *models.ScrapeOptions
	ResultChan chan JobResult
	Context    context.Context
	CreatedAt  time.Time
}

// Worker represents a single worker goroutine
type Worker struct {
	ID       int
	JobChan  chan ScrapeJob
	QuitChan chan bool
	Pool     *WorkerPool
	logger   *logrus.Logger
}

// WorkerPool manages multiple worker goroutines and job queue
type WorkerPool struct {
	config         *config.Config
	workers        []*Worker
	jobQueue       chan ScrapeJob
	dispatcher     *Dispatcher
	rateLimiter    *RateLimiter
	scraperFactory scraper.ScraperFactory
	logger         *logrus.Logger
	mu             sync.RWMutex
	running        bool
	stats          *PoolStats
}

// PoolStats tracks worker pool statistics
type PoolStats struct {
	mu                    sync.RWMutex
	JobsQueued            int64
	JobsProcessed         int64
	JobsSuccessful        int64
	JobsFailed            int64
	TotalProcessingTime   time.Duration
	AverageProcessingTime time.Duration
}

// NewWorkerPool creates a new worker pool instance
func NewWorkerPool(cfg *config.Config, scraperFactory scraper.ScraperFactory) *WorkerPool {
	logger := utils.GetLogger()

	pool := &WorkerPool{
		config:         cfg,
		jobQueue:       make(chan ScrapeJob, cfg.Workers.QueueSize),
		rateLimiter:    NewRateLimiter(cfg),
		scraperFactory: scraperFactory,
		logger:         logger,
		stats:          &PoolStats{},
	}

	// Initialize workers
	pool.workers = make([]*Worker, cfg.Workers.PoolSize)
	for i := 0; i < cfg.Workers.PoolSize; i++ {
		worker := &Worker{
			ID:       i + 1,
			JobChan:  make(chan ScrapeJob),
			QuitChan: make(chan bool),
			Pool:     pool,
			logger:   logger.WithField("worker_id", i+1).Logger,
		}
		pool.workers[i] = worker
	}

	// Initialize dispatcher
	pool.dispatcher = NewDispatcher(pool.jobQueue, pool.workers)

	logger.WithField("pool_size", cfg.Workers.PoolSize).Info("Worker pool initialized")
	return pool
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return fmt.Errorf("worker pool is already running")
	}

	wp.logger.Info("Starting worker pool")

	// Start dispatcher
	wp.dispatcher.Start()

	// Start all workers
	for _, worker := range wp.workers {
		go worker.Start()
	}

	wp.running = true
	wp.logger.WithField("workers", len(wp.workers)).Info("Worker pool started successfully")
	return nil
}

// Stop stops the worker pool gracefully
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return nil
	}

	wp.logger.Info("Stopping worker pool")

	// Stop dispatcher first
	wp.dispatcher.Stop()

	// Stop all workers
	for _, worker := range wp.workers {
		worker.Stop()
	}

	// Close job queue
	close(wp.jobQueue)

	wp.running = false
	wp.logger.Info("Worker pool stopped successfully")
	return nil
}

// SubmitJob submits a new scraping job to the pool
func (wp *WorkerPool) SubmitJob(ctx context.Context, url string, options *models.ScrapeOptions) (*JobResult, error) {
	if !wp.IsRunning() {
		return nil, fmt.Errorf("worker pool is not running")
	}

	// Check rate limit for the domain
	domain := extractDomain(url)
	if !wp.rateLimiter.Allow(domain) {
		return nil, fmt.Errorf("rate limit exceeded for domain: %s", domain)
	}

	// Create job
	job := ScrapeJob{
		ID:         utils.GenerateRequestID(),
		URL:        url,
		Options:    options,
		ResultChan: make(chan JobResult, 1),
		Context:    ctx,
		CreatedAt:  time.Now(),
	}

	// Update stats
	wp.stats.mu.Lock()
	wp.stats.JobsQueued++
	wp.stats.mu.Unlock()

	// Submit job to queue
	select {
	case wp.jobQueue <- job:
		wp.logger.WithFields(logrus.Fields{
			"job_id": job.ID,
			"url":    url,
		}).Info("Job submitted to queue")
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("job queue is full, request timed out")
	}

	// Wait for result with timeout
	timeout := wp.config.Workers.Timeout
	if options != nil && options.Timeout > 0 {
		timeout = options.Timeout
	}

	select {
	case result := <-job.ResultChan:
		return &result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("job processing timed out after %v", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// IsRunning returns true if the worker pool is running
func (wp *WorkerPool) IsRunning() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.running
}

// GetStats returns current pool statistics
func (wp *WorkerPool) GetStats() PoolStats {
	wp.stats.mu.RLock()
	defer wp.stats.mu.RUnlock()

	stats := *wp.stats
	if stats.JobsProcessed > 0 {
		stats.AverageProcessingTime = stats.TotalProcessingTime / time.Duration(stats.JobsProcessed)
	}

	return stats
}

// Start starts the worker goroutine
func (w *Worker) Start() {
	w.logger.Info("Worker started")

	for {
		select {
		case job := <-w.JobChan:
			w.processJob(job)
		case <-w.QuitChan:
			w.logger.Info("Worker stopping")
			return
		}
	}
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.QuitChan <- true
}

// processJob processes a single scraping job
func (w *Worker) processJob(job ScrapeJob) {
	startTime := time.Now()

	w.logger.WithFields(logrus.Fields{
		"job_id":    job.ID,
		"worker_id": w.ID,
		"url":       job.URL,
	}).Debug("Processing job")

	// Update stats
	w.Pool.stats.mu.Lock()
	w.Pool.stats.JobsProcessed++
	w.Pool.stats.mu.Unlock()

	// Process the job using the scraper
	result := w.scrapeJob(job)

	// Update processing time stats
	processingTime := time.Since(startTime)
	result.Duration = processingTime

	w.Pool.stats.mu.Lock()
	w.Pool.stats.TotalProcessingTime += processingTime
	if result.Error != nil {
		w.Pool.stats.JobsFailed++
	} else {
		w.Pool.stats.JobsSuccessful++
	}
	w.Pool.stats.mu.Unlock()

	// Send result back (non-blocking)
	select {
	case job.ResultChan <- result:
		w.logger.WithFields(logrus.Fields{
			"job_id":          job.ID,
			"worker_id":       w.ID,
			"processing_time": processingTime,
			"success":         result.Error == nil,
		}).Info("Job completed")
	case <-time.After(100 * time.Millisecond):
		w.logger.WithFields(logrus.Fields{
			"job_id":    job.ID,
			"worker_id": w.ID,
		}).Debug("Result channel timeout - client may have disconnected")
	}
}

// scrapeJob performs the actual scraping work
func (w *Worker) scrapeJob(job ScrapeJob) JobResult {
	result := JobResult{
		RequestID: job.ID,
	}

	// Determine the scraping engine
	engine := "headed" // Default engine
	if job.Options != nil && job.Options.Engine != "" {
		engine = job.Options.Engine
	}

	// Get domain for rate limiting
	domain := extractDomain(job.URL)

	// Create scraper instance
	scraper, err := w.Pool.scraperFactory.CreateScraper(engine)
	if err != nil {
		result.Error = fmt.Errorf("failed to create scraper: %w", err)
		w.Pool.rateLimiter.RecordFailure(domain, err)
		return result
	}
	defer scraper.Cleanup()

	// Retry logic
	maxRetries := w.Pool.config.Workers.MaxRetries
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			w.logger.WithFields(logrus.Fields{
				"job_id":    job.ID,
				"worker_id": w.ID,
				"attempt":   attempt + 1,
				"url":       job.URL,
			}).Debug("Retrying scraping job")

			// Exponential backoff
			backoffDelay := time.Duration(attempt) * time.Second
			time.Sleep(backoffDelay)
		}

		// Perform the scraping operation
		jobPosting, err := scraper.ScrapeJob(job.Context, job.URL, job.Options)
		if err != nil {
			lastErr = err
			w.logger.WithFields(logrus.Fields{
				"job_id":    job.ID,
				"worker_id": w.ID,
				"attempt":   attempt + 1,
				"error":     err.Error(),
			}).Debug("Scraping attempt failed")

			// Record failure for rate limiting
			w.Pool.rateLimiter.RecordFailure(domain, err)
			continue
		}

		// Success
		result.Job = jobPosting
		w.Pool.rateLimiter.RecordSuccess(domain)

		w.logger.WithFields(logrus.Fields{
			"job_id":    job.ID,
			"worker_id": w.ID,
			"job_title": jobPosting.Title,
			"company":   jobPosting.Company,
			"attempt":   attempt + 1,
		}).Debug("Scraping job completed successfully")

		return result
	}

	// All attempts failed
	result.Error = fmt.Errorf("scraping failed after %d attempts, last error: %w", maxRetries+1, lastErr)
	return result
}

// extractDomain extracts domain from URL for rate limiting
func extractDomain(url string) string {
	return extractDomainFromURL(url)
}
