package background

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
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
	appLogger    *logrus.Logger
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
	ExecuteFunc   func(context.Context) (*TaskResult, error)
	CompletedChan chan *TaskResult
}

// NewTaskManager creates a new task manager
func NewTaskManager(cfg *config.Config) *TaskManagerImpl {
	maxWorkers := 10
	maxQueueSize := 100

	// Use configuration values if available
	if cfg.Workers.PoolSize > 0 {
		maxWorkers = cfg.Workers.PoolSize
	}
	if cfg.Workers.QueueSize > 0 {
		maxQueueSize = cfg.Workers.QueueSize
	}

	return &TaskManagerImpl{
		config:       cfg,
		store:        NewInMemoryTaskStore(),
		logger:       NewTaskCompletionLogger(),
		appLogger:    utils.GetLogger(),
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

	tm.appLogger.WithField("max_workers", tm.maxWorkers).Info("Task manager started")
	return nil
}

// Stop stops the task manager gracefully
func (tm *TaskManagerImpl) Stop(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.running {
		return nil
	}

	tm.appLogger.Info("Stopping task manager...")

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
		tm.appLogger.Info("Task manager stopped gracefully")
	case <-ctx.Done():
		tm.appLogger.Warn("Task manager shutdown timed out")
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

	// Create task execution
	execution := &TaskExecution{
		ProcessID: processID,
		Type:      TaskTypeScrape,
		Context:   tm.ctx, // Use task manager's context, not HTTP request context
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

	// Create task execution
	execution := &TaskExecution{
		ProcessID: processID,
		Type:      TaskTypeTailor,
		Context:   tm.ctx, // Use task manager's context, not HTTP request context
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

	tm.appLogger.WithField("worker_id", workerID).Info("Task worker started")

	for {
		select {
		case <-tm.ctx.Done():
			tm.appLogger.WithField("worker_id", workerID).Info("Task worker stopping")
			return
		case task, ok := <-tm.taskChan:
			if !ok {
				tm.appLogger.WithField("worker_id", workerID).Info("Task channel closed, worker stopping")
				return
			}

			tm.processTask(workerID, task)
		}
	}
}

// processTask processes a single task
func (tm *TaskManagerImpl) processTask(workerID int, task *TaskExecution) {
	startTime := time.Now()

	tm.appLogger.WithFields(map[string]interface{}{
		"worker_id":  workerID,
		"process_id": task.ProcessID,
		"task_type":  task.Type,
	}).Info("Processing task")

	// Update task status to processing
	if err := tm.updateTaskStatus(task.ProcessID, TaskStatusProcessing); err != nil {
		tm.appLogger.WithError(err).Error("Failed to update task status to processing")
	}

	// Log task start
	tm.logger.LogTaskStart(task.ProcessID, task.Type)

	// Execute the task
	result, err := task.ExecuteFunc(task.Context)
	processingTime := time.Since(startTime)

	if err != nil {
		// Task failed
		tm.appLogger.WithFields(map[string]interface{}{
			"worker_id":       workerID,
			"process_id":      task.ProcessID,
			"task_type":       task.Type,
			"processing_time": processingTime,
			"error":           err.Error(),
		}).Error("Task execution failed")

		// Update task result with failure
		result = &TaskResult{
			ProcessID:      task.ProcessID,
			Type:           task.Type,
			Status:         TaskStatusFailure,
			Error:          err.Error(),
			CreatedAt:      time.Now(),
			ProcessingTime: processingTime,
		}

		tm.logger.LogTaskError(task.ProcessID, task.Type, err)
	} else {
		// Task succeeded
		tm.appLogger.WithFields(map[string]interface{}{
			"worker_id":       workerID,
			"process_id":      task.ProcessID,
			"task_type":       task.Type,
			"processing_time": processingTime,
		}).Info("Task execution completed successfully")

		result.Status = TaskStatusSuccess
		result.ProcessingTime = processingTime
		completedAt := time.Now()
		result.CompletedAt = &completedAt

		tm.logger.LogTaskSuccess(task.ProcessID, task.Type, processingTime)
	}

	// Store the final result
	if err := tm.store.Update(task.Context, result); err != nil {
		tm.appLogger.WithError(err).Error("Failed to store task result")
	}

	// Log structured completion to stdout
	if err := tm.logger.LogTaskCompletion(result); err != nil {
		tm.appLogger.WithError(err).Error("Failed to log task completion")
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
				tm.appLogger.WithError(err).Error("Failed to cleanup old task results")
			}
		}
	}
}

// executeScrapeTask executes a scrape task in the background
func (tm *TaskManagerImpl) executeScrapeTask(ctx context.Context, processID string, request models.ScrapeRequest, poolManager *workers.PoolManager) (*TaskResult, error) {
	startTime := time.Now()

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

	// Create successful task result
	taskResult := &TaskResult{
		ProcessID:      processID,
		Type:           TaskTypeScrape,
		Status:         TaskStatusSuccess,
		Data:           taskData,
		CreatedAt:      startTime,
		ProcessingTime: time.Since(startTime),
		Metadata: map[string]interface{}{
			"url":    request.URL,
			"engine": engine,
		},
	}

	return taskResult, nil
}

// executeTailorTask executes a tailor task in the background
func (tm *TaskManagerImpl) executeTailorTask(ctx context.Context, processID string, request models.TailorResumeRequest, llmManager *llm.Manager, cfg *config.Config) (*TaskResult, error) {
	startTime := time.Now()

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
				tm.appLogger.WithFields(map[string]interface{}{
					"process_id": processID,
					"error":      fmt.Sprintf("%v", r),
				}).Warn("Redis initialization failed - continuing without conversation history")
			}
		}()

		redisClient = utils.NewRedisClient(cfg)
		if err := redisClient.Ping(ctx); err != nil {
			tm.appLogger.WithFields(map[string]interface{}{
				"process_id": processID,
				"error":      err.Error(),
			}).Warn("Redis connection failed - conversation history will not be saved")
			if redisClient != nil {
				redisClient.Close()
				redisClient = nil
			}
		} else {
			redisAvailable = true
			tm.appLogger.WithFields(map[string]interface{}{
				"process_id": processID,
			}).Debug("Redis connection successful")
		}
	}()

	// Only use Redis if it's available
	if redisAvailable && redisClient != nil {
		defer redisClient.Close()

		// Create conversation thread with resumeID as threadID
		if err := redisClient.CreateConversationThread(ctx, request.ResumeID); err != nil {
			tm.appLogger.WithFields(map[string]interface{}{
				"process_id": processID,
				"resume_id":  request.ResumeID,
				"error":      err.Error(),
			}).Warn("Failed to create conversation thread - continuing without history")
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
		// Store the raw AI response and structured response
		// (Similar to the existing handler logic)
		// TODO: Implement conversation history storage in background task
	}

	// Create task data
	taskData := &TailorTaskData{
		TailoredResume: tailoredResume,
		Suggestions:    suggestions,
		ThreadID:       request.ResumeID,
	}

	// Create successful task result
	taskResult := &TaskResult{
		ProcessID:      processID,
		Type:           TaskTypeTailor,
		Status:         TaskStatusSuccess,
		Data:           taskData,
		CreatedAt:      startTime,
		ProcessingTime: time.Since(startTime),
		Metadata: map[string]interface{}{
			"resume_id": request.ResumeID,
			"job_title": request.Job.Title,
			"company":   request.Job.CompanyName,
		},
	}

	return taskResult, nil
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
