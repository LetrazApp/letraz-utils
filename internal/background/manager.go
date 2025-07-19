package background

import (
	"context"
	"fmt"
	"sync"
	"time"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/internal/scraper/engines/headed"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// Task manager configuration constants
const (
	// Default configuration values
	DefaultMaxWorkers   = 10
	DefaultMaxQueueSize = 100

	// Minimum configuration values to prevent misconfiguration
	MinWorkers   = 1
	MinQueueSize = 1

	// Maximum configuration values for safety
	MaxWorkers   = 1000
	MaxQueueSize = 10000
)

// TaskManager defines the interface for managing background tasks
type TaskManager interface {
	// Start starts the task manager
	Start(ctx context.Context) error

	// Stop stops the task manager gracefully
	Stop(ctx context.Context) error

	// SubmitScrapeTask submits a scrape task for background processing
	SubmitScrapeTask(ctx context.Context, processID string, request models.ScrapeRequest, poolManager *workers.PoolManager) error

	// SubmitTailorTask submits a tailor task for background processing
	SubmitTailorTask(ctx context.Context, processID string, request models.TailorResumeRequest, llmManager *llm.Manager, cfg *config.Config) error

	// SubmitScreenshotTask submits a screenshot task for background processing
	SubmitScreenshotTask(ctx context.Context, processID string, request models.ResumeScreenshotRequest, cfg *config.Config) error

	// GetTaskResult retrieves the result of a task by process ID
	GetTaskResult(ctx context.Context, processID string) (*TaskResult, error)

	// GetTaskStatus retrieves the status of a task by process ID
	GetTaskStatus(ctx context.Context, processID string) (TaskStatus, error)

	// ListTasks lists all active tasks (for monitoring)
	ListTasks(ctx context.Context) ([]*TaskResult, error)

	// IsHealthy checks if the task manager is healthy
	IsHealthy() bool
}

// TaskManagerImpl implements the TaskManager interface
type TaskManagerImpl struct {
	config       *config.Config
	store        TaskStore
	logger       *TaskCompletionLogger
	appLogger    types.Logger
	workerPool   chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	running      bool
	taskChan     chan *TaskExecution
	maxWorkers   int
	maxQueueSize int
}

// TaskExecution represents a task execution context
type TaskExecution struct {
	ProcessID     string
	Type          TaskType
	Context       context.Context
	Cancel        context.CancelFunc
	ExecuteFunc   func(context.Context) (*TaskResult, error)
	CompletedChan chan *TaskResult
}

// validateTaskManagerConfig validates and returns safe configuration values
func validateTaskManagerConfig(cfg *config.Config) (maxWorkers, maxQueueSize int, err error) {
	// Validate and set worker pool size
	maxWorkers = cfg.Workers.PoolSize
	if maxWorkers <= 0 {
		maxWorkers = DefaultMaxWorkers
	} else if maxWorkers < MinWorkers {
		return 0, 0, fmt.Errorf("worker pool size (%d) is below minimum (%d)", maxWorkers, MinWorkers)
	} else if maxWorkers > MaxWorkers {
		return 0, 0, fmt.Errorf("worker pool size (%d) exceeds maximum (%d)", maxWorkers, MaxWorkers)
	}

	// Validate and set queue size
	maxQueueSize = cfg.Workers.QueueSize
	if maxQueueSize <= 0 {
		maxQueueSize = DefaultMaxQueueSize
	} else if maxQueueSize < MinQueueSize {
		return 0, 0, fmt.Errorf("queue size (%d) is below minimum (%d)", maxQueueSize, MinQueueSize)
	} else if maxQueueSize > MaxQueueSize {
		return 0, 0, fmt.Errorf("queue size (%d) exceeds maximum (%d)", maxQueueSize, MaxQueueSize)
	}

	return maxWorkers, maxQueueSize, nil
}

// NewTaskManager creates a new task manager
func NewTaskManager(cfg *config.Config) *TaskManagerImpl {
	logger := logging.GetGlobalLogger()

	// Validate configuration and get safe values
	maxWorkers, maxQueueSize, err := validateTaskManagerConfig(cfg)
	if err != nil {
		// Log validation error and fall back to defaults
		logger.Warn("Task manager configuration validation failed, using defaults", map[string]interface{}{
			"error": err.Error(),
		})
		maxWorkers = DefaultMaxWorkers
		maxQueueSize = DefaultMaxQueueSize
	}

	// Log final configuration values
	logger.Info("Task manager configuration initialized", map[string]interface{}{
		"max_workers":    maxWorkers,
		"max_queue_size": maxQueueSize,
		"using_defaults": err != nil,
	})

	return &TaskManagerImpl{
		config:       cfg,
		store:        NewInMemoryTaskStore(),
		logger:       NewTaskCompletionLogger(),
		appLogger:    logger,
		workerPool:   make(chan struct{}, maxWorkers),
		maxWorkers:   maxWorkers,
		maxQueueSize: maxQueueSize,
		taskChan:     make(chan *TaskExecution, maxQueueSize),
	}
}

// Start starts the task manager
func (tm *TaskManagerImpl) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.running {
		return fmt.Errorf("task manager already running")
	}

	tm.ctx, tm.cancel = context.WithCancel(ctx)
	tm.running = true

	// Start worker goroutines
	for i := 0; i < tm.maxWorkers; i++ {
		tm.wg.Add(1)
		go tm.worker(i)
	}

	// Start cleanup goroutine
	tm.wg.Add(1)
	go tm.cleanupRoutine()

	tm.appLogger.Info("Task manager started", map[string]interface{}{
		"max_workers": tm.maxWorkers,
	})
	return nil
}

// Stop stops the task manager gracefully
func (tm *TaskManagerImpl) Stop(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.running {
		return nil
	}

	tm.appLogger.Info("Stopping task manager...", map[string]interface{}{})

	// Cancel context to signal workers to stop
	tm.cancel()

	// Close task channel
	close(tm.taskChan)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		tm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		tm.appLogger.Info("Task manager stopped gracefully", map[string]interface{}{})
	case <-ctx.Done():
		tm.appLogger.Warn("Task manager shutdown timed out", map[string]interface{}{})
	}

	tm.running = false
	return nil
}

// SubmitScrapeTask submits a scrape task for background processing
func (tm *TaskManagerImpl) SubmitScrapeTask(ctx context.Context, processID string, request models.ScrapeRequest, poolManager *workers.PoolManager) error {
	if !tm.IsHealthy() {
		return fmt.Errorf("task manager is not healthy")
	}

	// Create task result
	result := &TaskResult{
		ProcessID: processID,
		Type:      TaskTypeScrape,
		Status:    TaskStatusAccepted,
		CreatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"url":    request.URL,
			"engine": getEngineFromOptions(request.Options),
		},
	}

	// Store initial task result
	if err := tm.store.Store(ctx, result); err != nil {
		return fmt.Errorf("failed to store task result: %w", err)
	}

	// Log task acceptance
	tm.logger.LogTaskAccepted(processID, TaskTypeScrape)

	// Create task execution with derived context for better isolation
	taskCtx, cancelFunc := context.WithCancel(tm.ctx)
	execution := &TaskExecution{
		ProcessID: processID,
		Type:      TaskTypeScrape,
		Context:   taskCtx, // Use derived context for task isolation
		Cancel:    cancelFunc,
		ExecuteFunc: func(execCtx context.Context) (*TaskResult, error) {
			return tm.executeScrapeTask(execCtx, processID, request, poolManager)
		},
		CompletedChan: make(chan *TaskResult, 1),
	}

	// Submit to worker pool
	select {
	case tm.taskChan <- execution:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("task queue is full")
	}
}

// SubmitTailorTask submits a tailor task for background processing
func (tm *TaskManagerImpl) SubmitTailorTask(ctx context.Context, processID string, request models.TailorResumeRequest, llmManager *llm.Manager, cfg *config.Config) error {
	if !tm.IsHealthy() {
		return fmt.Errorf("task manager is not healthy")
	}

	// Create task result
	result := &TaskResult{
		ProcessID: processID,
		Type:      TaskTypeTailor,
		Status:    TaskStatusAccepted,
		CreatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"resume_id": request.ResumeID,
			"job_title": request.Job.Title,
			"company":   request.Job.CompanyName,
		},
	}

	// Store initial task result
	if err := tm.store.Store(ctx, result); err != nil {
		return fmt.Errorf("failed to store task result: %w", err)
	}

	// Log task acceptance
	tm.logger.LogTaskAccepted(processID, TaskTypeTailor)

	// Create task execution with derived context for better isolation
	taskCtx, cancelFunc := context.WithCancel(tm.ctx)
	execution := &TaskExecution{
		ProcessID: processID,
		Type:      TaskTypeTailor,
		Context:   taskCtx, // Use derived context for task isolation
		Cancel:    cancelFunc,
		ExecuteFunc: func(execCtx context.Context) (*TaskResult, error) {
			return tm.executeTailorTask(execCtx, processID, request, llmManager, cfg)
		},
		CompletedChan: make(chan *TaskResult, 1),
	}

	// Submit to worker pool
	select {
	case tm.taskChan <- execution:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("task queue is full")
	}
}

// SubmitScreenshotTask submits a screenshot task for background processing
func (tm *TaskManagerImpl) SubmitScreenshotTask(ctx context.Context, processID string, request models.ResumeScreenshotRequest, cfg *config.Config) error {
	if !tm.IsHealthy() {
		return fmt.Errorf("task manager is not healthy")
	}

	// Create task result
	result := &TaskResult{
		ProcessID: processID,
		Type:      TaskTypeScreenshot,
		Status:    TaskStatusAccepted,
		CreatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"resume_id": request.ResumeID,
		},
	}

	// Store initial task result
	if err := tm.store.Store(ctx, result); err != nil {
		return fmt.Errorf("failed to store task result: %w", err)
	}

	// Log task acceptance
	tm.logger.LogTaskAccepted(processID, TaskTypeScreenshot)

	// Create task execution with derived context for better isolation
	taskCtx, cancelFunc := context.WithCancel(tm.ctx)
	execution := &TaskExecution{
		ProcessID: processID,
		Type:      TaskTypeScreenshot,
		Context:   taskCtx, // Use derived context for task isolation
		Cancel:    cancelFunc,
		ExecuteFunc: func(execCtx context.Context) (*TaskResult, error) {
			return tm.executeScreenshotTask(execCtx, processID, request, cfg)
		},
		CompletedChan: make(chan *TaskResult, 1),
	}

	// Submit to worker pool
	select {
	case tm.taskChan <- execution:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("task queue is full")
	}
}

// GetTaskResult retrieves the result of a task by process ID
func (tm *TaskManagerImpl) GetTaskResult(ctx context.Context, processID string) (*TaskResult, error) {
	return tm.store.Get(ctx, processID)
}

// GetTaskStatus retrieves the status of a task by process ID
func (tm *TaskManagerImpl) GetTaskStatus(ctx context.Context, processID string) (TaskStatus, error) {
	result, err := tm.store.Get(ctx, processID)
	if err != nil {
		return "", err
	}
	return result.Status, nil
}

// ListTasks lists all active tasks (for monitoring)
func (tm *TaskManagerImpl) ListTasks(ctx context.Context) ([]*TaskResult, error) {
	return tm.store.List(ctx)
}

// IsHealthy checks if the task manager is healthy
func (tm *TaskManagerImpl) IsHealthy() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.running && tm.ctx.Err() == nil
}

// worker processes tasks from the task channel
func (tm *TaskManagerImpl) worker(workerID int) {
	defer tm.wg.Done()

	tm.appLogger.Info("Task worker started", map[string]interface{}{
		"worker_id": workerID,
	})

	for {
		select {
		case <-tm.ctx.Done():
			tm.appLogger.Info("Task worker stopping", map[string]interface{}{
				"worker_id": workerID,
			})
			return
		case task, ok := <-tm.taskChan:
			if !ok {
				tm.appLogger.Info("Task channel closed, worker stopping", map[string]interface{}{
					"worker_id": workerID,
				})
				return
			}

			tm.processTask(workerID, task)
		}
	}
}

// processTask processes a single task
func (tm *TaskManagerImpl) processTask(workerID int, task *TaskExecution) {
	startTime := time.Now()

	tm.appLogger.Info("Processing task", map[string]interface{}{
		"worker_id":  workerID,
		"process_id": task.ProcessID,
		"task_type":  task.Type,
	})

	// Update task status to processing
	if err := tm.updateTaskStatus(task.ProcessID, TaskStatusProcessing); err != nil {
		tm.appLogger.Error("Failed to update task status to processing", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Log task start
	tm.logger.LogTaskStart(task.ProcessID, task.Type)

	// Execute the task
	result, err := task.ExecuteFunc(task.Context)
	processingTime := time.Since(startTime)

	if err != nil {
		// Task failed
		tm.appLogger.Error("Task execution failed", map[string]interface{}{
			"worker_id":       workerID,
			"process_id":      task.ProcessID,
			"task_type":       task.Type,
			"processing_time": processingTime,
			"error":           err.Error(),
		})

		// Retrieve existing task result to preserve original CreatedAt
		existingResult, getErr := tm.store.Get(task.Context, task.ProcessID)
		if getErr != nil {
			tm.appLogger.Error("Failed to retrieve existing task result for failure update", map[string]interface{}{
				"error": getErr.Error(),
			})
			// Fallback: create new result (preserving old behavior)
			result = &TaskResult{
				ProcessID:      task.ProcessID,
				Type:           task.Type,
				Status:         TaskStatusFailure,
				Error:          err.Error(),
				CreatedAt:      time.Now(),
				ProcessingTime: &processingTime,
			}
		} else {
			// Update existing result with failure data
			existingResult.Status = TaskStatusFailure
			existingResult.Error = err.Error()
			existingResult.ProcessingTime = &processingTime
			result = existingResult
		}

		tm.logger.LogTaskError(task.ProcessID, task.Type, err)
	} else {
		// Task succeeded
		tm.appLogger.Info("Task execution completed successfully", map[string]interface{}{
			"worker_id":       workerID,
			"process_id":      task.ProcessID,
			"task_type":       task.Type,
			"processing_time": processingTime,
		})

		result.Status = TaskStatusSuccess
		result.ProcessingTime = &processingTime
		completedAt := time.Now()
		result.CompletedAt = &completedAt

		tm.logger.LogTaskSuccess(task.ProcessID, task.Type, processingTime)
	}

	// Store the final result
	if err := tm.store.Update(task.Context, result); err != nil {
		tm.appLogger.Error("Failed to store task result", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Log structured completion to stdout
	if err := tm.logger.LogTaskCompletion(result); err != nil {
		tm.appLogger.Error("Failed to log task completion", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Cancel the task context to prevent context leaks
	if task.Cancel != nil {
		task.Cancel()
	}
}

// updateTaskStatus updates the status of a task
func (tm *TaskManagerImpl) updateTaskStatus(processID string, status TaskStatus) error {
	result, err := tm.store.Get(context.Background(), processID)
	if err != nil {
		return err
	}

	result.Status = status
	return tm.store.Update(context.Background(), result)
}

// cleanupRoutine periodically cleans up old task results
func (tm *TaskManagerImpl) cleanupRoutine() {
	defer tm.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			maxAge := 24 * time.Hour // Keep results for 24 hours
			if err := tm.store.Cleanup(context.Background(), maxAge); err != nil {
				tm.appLogger.Error("Failed to cleanup old task results", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}
}

// executeScrapeTask executes a scrape task in the background
func (tm *TaskManagerImpl) executeScrapeTask(ctx context.Context, processID string, request models.ScrapeRequest, poolManager *workers.PoolManager) (*TaskResult, error) {
	startTime := time.Now()

	// Retrieve the existing task result to preserve original CreatedAt
	existingResult, err := tm.store.Get(ctx, processID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve existing task result: %w", err)
	}

	// Execute the scraping job using the existing worker pool
	result, err := poolManager.SubmitJob(ctx, request.URL, request.Options)
	if err != nil {
		return nil, fmt.Errorf("failed to submit scraping job: %w", err)
	}

	// Determine engine used
	engine := getEngineFromOptions(request.Options)

	// Create task result based on scraping result
	var taskData interface{}

	if result.Error != nil {
		// Scraping failed
		return nil, result.Error
	}

	// Scraping succeeded - create appropriate task data
	if result.UsedLLM && result.Job != nil {
		// New LLM-processed job
		taskData = &ScrapeTaskData{
			Job:     result.Job,
			Engine:  engine + "_llm",
			UsedLLM: true,
		}
	} else if result.JobPosting != nil {
		// Legacy job posting
		taskData = &ScrapeTaskData{
			JobPosting: result.JobPosting,
			Engine:     engine + "_legacy",
			UsedLLM:    false,
		}
	} else {
		return nil, fmt.Errorf("job processing completed but no data was returned")
	}

	// Update the existing task result with success data
	processingTime := time.Since(startTime)
	existingResult.Status = TaskStatusSuccess
	existingResult.Data = taskData
	existingResult.ProcessingTime = &processingTime
	existingResult.Metadata = map[string]interface{}{
		"url":    request.URL,
		"engine": engine,
	}

	return existingResult, nil
}

// executeTailorTask executes a tailor task in the background
func (tm *TaskManagerImpl) executeTailorTask(ctx context.Context, processID string, request models.TailorResumeRequest, llmManager *llm.Manager, cfg *config.Config) (*TaskResult, error) {
	startTime := time.Now()

	// Retrieve the existing task result to preserve original CreatedAt
	existingResult, err := tm.store.Get(ctx, processID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve existing task result: %w", err)
	}

	// Check LLM manager health
	if !llmManager.IsHealthy() {
		return nil, fmt.Errorf("LLM manager is not healthy")
	}

	// Initialize Redis client for conversation history (optional)
	var redisClient *utils.RedisClient
	var redisAvailable bool

	// Try to initialize Redis, but don't fail if it's not available
	func() {
		defer func() {
			if r := recover(); r != nil {
				tm.appLogger.Warn("Redis initialization failed - continuing without conversation history", map[string]interface{}{
					"process_id": processID,
					"error":      fmt.Sprintf("%v", r),
				})
			}
		}()

		redisClient = utils.NewRedisClient(cfg)
		if err := redisClient.Ping(ctx); err != nil {
			tm.appLogger.Warn("Redis connection failed - conversation history will not be saved", map[string]interface{}{
				"process_id": processID,
				"error":      err.Error(),
			})
			if redisClient != nil {
				redisClient.Close()
				redisClient = nil
			}
		} else {
			redisAvailable = true
			tm.appLogger.Debug("Redis connection successful", map[string]interface{}{
				"process_id": processID,
			})
		}
	}()

	// Only use Redis if it's available
	if redisAvailable && redisClient != nil {
		defer redisClient.Close()

		// Create conversation thread with resumeID as threadID
		if err := redisClient.CreateConversationThread(ctx, request.ResumeID); err != nil {
			tm.appLogger.Warn("Failed to create conversation thread - continuing without history", map[string]interface{}{
				"process_id": processID,
				"resume_id":  request.ResumeID,
				"error":      err.Error(),
			})
		}

		// Store conversation history entries...
		// (Similar to the existing handler logic)
	}

	// Call LLM to tailor the resume
	tailoredResume, suggestions, _, err := llmManager.TailorResumeWithRawResponse(ctx, &request.BaseResume, &request.Job)
	if err != nil {
		return nil, fmt.Errorf("failed to tailor resume using LLM: %w", err)
	}

	// Store AI response in conversation history (if Redis is available)
	if redisAvailable && redisClient != nil {
		// TODO: Implement conversation history storage in background task
		tm.appLogger.Debug("Conversation history storage not yet implemented in background task", map[string]interface{}{
			"process_id": processID,
			"resume_id":  request.ResumeID,
		})
	}

	// Create task data
	taskData := &TailorTaskData{
		TailoredResume: tailoredResume,
		Suggestions:    suggestions,
		ThreadID:       request.ResumeID,
	}

	// Update the existing task result with success data
	processingTime := time.Since(startTime)
	existingResult.Status = TaskStatusSuccess
	existingResult.Data = taskData
	existingResult.ProcessingTime = &processingTime
	existingResult.Metadata = map[string]interface{}{
		"resume_id": request.ResumeID,
		"job_title": request.Job.Title,
		"company":   request.Job.CompanyName,
	}

	return existingResult, nil
}

// executeScreenshotTask executes a screenshot task in the background
func (tm *TaskManagerImpl) executeScreenshotTask(ctx context.Context, processID string, request models.ResumeScreenshotRequest, cfg *config.Config) (*TaskResult, error) {
	startTime := time.Now()

	// Retrieve the existing task result to preserve original CreatedAt
	existingResult, err := tm.store.Get(ctx, processID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve existing task result: %w", err)
	}

	tm.appLogger.Info("Starting screenshot generation", map[string]interface{}{
		"process_id": processID,
		"resume_id":  request.ResumeID,
	})

	// Create screenshot service
	screenshotService := headed.NewScreenshotService(cfg)
	defer screenshotService.Cleanup()

	// Check if screenshot service is healthy
	if !screenshotService.IsHealthy() {
		return nil, fmt.Errorf("screenshot service is not healthy")
	}

	// Create DigitalOcean Spaces client
	spacesClient, err := utils.NewSpacesClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create DigitalOcean Spaces client: %w", err)
	}

	// Check if Spaces client is healthy
	if !spacesClient.IsHealthy() {
		return nil, fmt.Errorf("DigitalOcean Spaces is not healthy")
	}

	// Capture the screenshot
	screenshotData, err := screenshotService.CaptureResumeScreenshot(ctx, request.ResumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	// Upload screenshot to DigitalOcean Spaces
	screenshotURL, err := spacesClient.UploadScreenshot(request.ResumeID, screenshotData)
	if err != nil {
		return nil, fmt.Errorf("failed to upload screenshot: %w", err)
	}

	tm.appLogger.Info("Screenshot generated successfully", map[string]interface{}{
		"process_id":     processID,
		"resume_id":      request.ResumeID,
		"screenshot_url": screenshotURL,
		"file_size":      len(screenshotData),
	})

	// Create task data
	taskData := &ScreenshotTaskData{
		ScreenshotURL: screenshotURL,
		ResumeID:      request.ResumeID,
		FileSize:      len(screenshotData),
	}

	// Update the existing task result with success data
	processingTime := time.Since(startTime)
	existingResult.Status = TaskStatusSuccess
	existingResult.Data = taskData
	existingResult.ProcessingTime = &processingTime
	existingResult.Metadata = map[string]interface{}{
		"resume_id":      request.ResumeID,
		"screenshot_url": screenshotURL,
		"file_size":      len(screenshotData),
	}

	return existingResult, nil
}

// getEngineFromOptions extracts the engine from scrape options
func getEngineFromOptions(options *models.ScrapeOptions) string {
	if options == nil {
		return "hybrid"
	}
	if options.Engine == "" {
		return "hybrid"
	}
	return options.Engine
}
