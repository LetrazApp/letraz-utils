package workers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// JobResult represents the result of a scraping job
type JobResult struct {
	Job        *models.Job        // New Job structure with LLM processing
	JobPosting *models.JobPosting // Legacy JobPosting for backward compatibility
	Error      error
	RequestID  string
	Duration   time.Duration
	UsedLLM    bool // Flag to indicate if LLM was used
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
	logger   logging.Logger
}

// WorkerPool manages multiple worker goroutines and job queue
type WorkerPool struct {
	config         *config.Config
	workers        []*Worker
	jobQueue       chan ScrapeJob
	dispatcher     *Dispatcher
	rateLimiter    *RateLimiter
	scraperFactory scraper.ScraperFactory
	logger         logging.Logger
	mu             sync.RWMutex
	running        bool
	stats          *PoolStats
}

// PoolStats tracks worker pool statistics (internal use with mutex)
type PoolStats struct {
	mu                    sync.RWMutex
	JobsQueued            int64
	JobsProcessed         int64
	JobsSuccessful        int64
	JobsFailed            int64
	TotalProcessingTime   time.Duration
	AverageProcessingTime time.Duration
}

// PoolStatsData represents pool statistics for external consumption (no mutex)
type PoolStatsData struct {
	JobsQueued            int64         `json:"jobs_queued"`
	JobsProcessed         int64         `json:"jobs_processed"`
	JobsSuccessful        int64         `json:"jobs_successful"`
	JobsFailed            int64         `json:"jobs_failed"`
	TotalProcessingTime   time.Duration `json:"total_processing_time"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
}

// NewWorkerPool creates a new worker pool instance
func NewWorkerPool(cfg *config.Config, scraperFactory scraper.ScraperFactory) *WorkerPool {
	logger := logging.GetGlobalLogger()

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
			logger:   logger,
		}
		pool.workers[i] = worker
	}

	// Initialize dispatcher
	pool.dispatcher = NewDispatcher(pool.jobQueue, pool.workers)

	logger.Info("Worker pool initialized", map[string]interface{}{
		"pool_size": cfg.Workers.PoolSize,
	})
	return pool
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return fmt.Errorf("worker pool is already running")
	}

	wp.logger.Info("Starting worker pool", nil)
	wp.logger.Debug("DEBUG: About to start workers", map[string]interface{}{
		"worker_count": len(wp.workers),
	})

	// Start dispatcher
	wp.dispatcher.Start()
	wp.logger.Debug("DEBUG: Dispatcher started", nil)

	// Start workers
	for i, worker := range wp.workers {
		go worker.Start()
		wp.logger.Debug("DEBUG: Worker started", map[string]interface{}{
			"worker_id": worker.ID,
			"index":     i,
		})
	}

	wp.running = true
	wp.logger.Info("Worker pool started successfully", map[string]interface{}{
		"workers": len(wp.workers),
	})
	wp.logger.Debug("DEBUG: Worker pool start completed", nil)
	return nil
}

// Stop stops the worker pool gracefully
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return nil
	}

	wp.logger.Info("Stopping worker pool", nil)

	// Stop dispatcher first
	wp.dispatcher.Stop()

	// Stop all workers
	for _, worker := range wp.workers {
		worker.Stop()
	}

	// Close job queue
	close(wp.jobQueue)

	wp.running = false
	wp.logger.Info("Worker pool stopped successfully", nil)
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
		wp.logger.Info("Job submitted to queue", map[string]interface{}{
			"job_id": job.ID,
			"url":    url,
		})
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
func (wp *WorkerPool) GetStats() PoolStatsData {
	wp.stats.mu.RLock()
	defer wp.stats.mu.RUnlock()

	// Create a copy without the mutex to avoid copying lock value
	stats := PoolStatsData{
		JobsQueued:            wp.stats.JobsQueued,
		JobsProcessed:         wp.stats.JobsProcessed,
		JobsSuccessful:        wp.stats.JobsSuccessful,
		JobsFailed:            wp.stats.JobsFailed,
		TotalProcessingTime:   wp.stats.TotalProcessingTime,
		AverageProcessingTime: wp.stats.AverageProcessingTime,
	}

	if stats.JobsProcessed > 0 {
		stats.AverageProcessingTime = stats.TotalProcessingTime / time.Duration(stats.JobsProcessed)
	}

	return stats
}

// Start starts the worker goroutine
func (w *Worker) Start() {
	w.logger.Info("Worker started", nil)

	for {
		select {
		case job := <-w.JobChan:
			w.processJob(job)
		case <-w.QuitChan:
			w.logger.Info("Worker stopping", nil)
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

	// Send result back (non-blocking with reasonable timeout)
	select {
	case job.ResultChan <- result:
	case <-time.After(5 * time.Second):
		w.logger.Error("WORKER: Result channel timeout - failed to send result back to client", map[string]interface{}{
			"job_id":    job.ID,
			"worker_id": w.ID,
		})
	}
}

// scrapeJob performs the actual scraping work
func (w *Worker) scrapeJob(job ScrapeJob) JobResult {
	result := JobResult{
		RequestID: job.ID,
	}

	// Determine the scraping engine
	engine := "hybrid" // Default engine
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
			w.logger.Debug("Retrying scraping job", map[string]interface{}{
				"job_id":    job.ID,
				"worker_id": w.ID,
				"attempt":   attempt + 1,
				"url":       job.URL,
			})

			// Exponential backoff
			backoffDelay := time.Duration(attempt) * time.Second
			time.Sleep(backoffDelay)
		}

		// Determine if we should use LLM processing based on options
		useLLM := true // Default to LLM processing
		if job.Options != nil && job.Options.LLMProvider == "disabled" {
			useLLM = false
		}

		if useLLM {
			// Perform the scraping operation with LLM processing
			jobData, err := scraper.ScrapeJob(job.Context, job.URL, job.Options)

			if err != nil {
				// Return LLM errors directly to the client - no fallback
				result.Error = err
				result.UsedLLM = true

				// For "not job posting" errors, this is actually a successful determination
				if customErr, ok := err.(*utils.CustomError); ok && customErr.Code == http.StatusUnprocessableEntity {
					w.Pool.rateLimiter.RecordSuccess(domain)
					w.logger.Info("LLM successfully determined content is not a job posting", map[string]interface{}{
						"job_id":    job.ID,
						"worker_id": w.ID,
						"attempt":   attempt + 1,
						"mode":      "llm",
						"reason":    "not_job_posting",
					})

					// Don't retry "not job posting" errors - return immediately
					return result
				}

				// Check if this is a non-retryable error (API auth, missing key, etc.)
				if isNonRetryableError(err) {
					w.Pool.rateLimiter.RecordFailure(domain, err)
					w.logger.Error("LLM processing failed with non-retryable error", map[string]interface{}{
						"job_id":    job.ID,
						"worker_id": w.ID,
						"attempt":   attempt + 1,
						"error":     err.Error(),
						"mode":      "llm",
						"reason":    "non_retryable_error",
					})

					// Return immediately for non-retryable errors
					return result
				}

				// For retryable errors, record failure and continue retry loop
				lastErr = err
				w.Pool.rateLimiter.RecordFailure(domain, err)
				w.logger.Debug("LLM processing failed, will retry", map[string]interface{}{
					"job_id":    job.ID,
					"worker_id": w.ID,
					"attempt":   attempt + 1,
					"error":     err.Error(),
					"mode":      "llm",
				})

				// Continue to retry for technical failures
				continue
			}

			// Success with LLM
			result.Job = jobData
			result.UsedLLM = true
			w.Pool.rateLimiter.RecordSuccess(domain)

			return result
		} else {
			// Perform the scraping operation with legacy processing
			jobPosting, err := scraper.ScrapeJobLegacy(job.Context, job.URL, job.Options)
			if err != nil {
				lastErr = err
				w.logger.Debug("Scraping attempt failed (legacy mode)", map[string]interface{}{
					"job_id":    job.ID,
					"worker_id": w.ID,
					"attempt":   attempt + 1,
					"error":     err.Error(),
					"mode":      "legacy",
				})

				// Record failure for rate limiting
				w.Pool.rateLimiter.RecordFailure(domain, err)
				continue
			}

			// Success with legacy processing
			result.JobPosting = jobPosting
			result.UsedLLM = false
			w.Pool.rateLimiter.RecordSuccess(domain)

			w.logger.Debug("Scraping job completed successfully (legacy mode)", map[string]interface{}{
				"job_id":    job.ID,
				"worker_id": w.ID,
				"job_title": jobPosting.Title,
				"company":   jobPosting.Company,
				"attempt":   attempt + 1,
				"mode":      "legacy",
			})
		}

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

// isNonRetryableError determines if an error should not be retried
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Content validation errors
	if strings.Contains(errStr, "content is not a job posting") ||
		strings.Contains(errStr, "not a job posting") ||
		strings.Contains(errStr, "low confidence") {
		return true
	}

	// API authentication and authorization errors
	if strings.Contains(errStr, "api key") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "forbidden") ||
		strings.Contains(errStr, "invalid api key") ||
		strings.Contains(errStr, "permission denied") {
		return true
	}

	// Rate limiting errors that indicate we should stop for now
	if strings.Contains(errStr, "rate limit exceeded") ||
		strings.Contains(errStr, "quota exceeded") ||
		strings.Contains(errStr, "too many requests") {
		return true
	}

	// Malformed request errors
	if strings.Contains(errStr, "bad request") ||
		strings.Contains(errStr, "invalid request") ||
		strings.Contains(errStr, "malformed") {
		return true
	}

	// Service completely unavailable
	if strings.Contains(errStr, "service unavailable") ||
		strings.Contains(errStr, "not available") {
		return true
	}

	return false
}
