package background

import (
	"context"
	"sync"
	"time"

	"letraz-utils/pkg/models"
)

// TaskStatus represents the status of a background task
type TaskStatus string

const (
	TaskStatusAccepted   TaskStatus = "ACCEPTED"
	TaskStatusProcessing TaskStatus = "PROCESSING"
	TaskStatusSuccess    TaskStatus = "SUCCESS"
	TaskStatusFailure    TaskStatus = "FAILURE"
)

// TaskType represents the type of background task
type TaskType string

const (
	TaskTypeScrape TaskType = "scrape"
	TaskTypeTailor TaskType = "tailor"
)

// TaskResult represents the result of a background task
type TaskResult struct {
	ProcessID      string                 `json:"processId"`
	Type           TaskType               `json:"type"`
	Status         TaskStatus             `json:"status"`
	Data           interface{}            `json:"data,omitempty"`
	Error          string                 `json:"error,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	CompletedAt    *time.Time             `json:"completedAt,omitempty"`
	ProcessingTime time.Duration          `json:"processingTime,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ScrapeTaskData represents the data structure for scrape task results
type ScrapeTaskData struct {
	Job        *models.Job        `json:"job,omitempty"`
	JobPosting *models.JobPosting `json:"job_posting,omitempty"`
	Engine     string             `json:"engine"`
	UsedLLM    bool               `json:"used_llm"`
}

// TailorTaskData represents the data structure for tailor task results
type TailorTaskData struct {
	TailoredResume *models.TailoredResume `json:"tailored_resume,omitempty"`
	Suggestions    []models.Suggestion    `json:"suggestions,omitempty"`
	ThreadID       string                 `json:"thread_id,omitempty"`
}

// TaskStore defines the interface for storing and retrieving task results
type TaskStore interface {
	// Store stores a task result
	Store(ctx context.Context, result *TaskResult) error

	// Get retrieves a task result by process ID
	Get(ctx context.Context, processID string) (*TaskResult, error)

	// Update updates a task result
	Update(ctx context.Context, result *TaskResult) error

	// Delete removes a task result
	Delete(ctx context.Context, processID string) error

	// Cleanup removes expired task results
	Cleanup(ctx context.Context, maxAge time.Duration) error

	// List returns all task results (for monitoring)
	List(ctx context.Context) ([]*TaskResult, error)
}

// InMemoryTaskStore implements TaskStore using in-memory storage
type InMemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*TaskResult
}

// NewInMemoryTaskStore creates a new in-memory task store
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		tasks: make(map[string]*TaskResult),
	}
}

// Store stores a task result
func (s *InMemoryTaskStore) Store(ctx context.Context, result *TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[result.ProcessID] = result
	return nil
}

// Get retrieves a task result by process ID
func (s *InMemoryTaskStore) Get(ctx context.Context, processID string) (*TaskResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, exists := s.tasks[processID]
	if !exists {
		return nil, ErrTaskNotFound
	}

	return result, nil
}

// Update updates a task result
func (s *InMemoryTaskStore) Update(ctx context.Context, result *TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[result.ProcessID]; !exists {
		return ErrTaskNotFound
	}

	s.tasks[result.ProcessID] = result
	return nil
}

// Delete removes a task result
func (s *InMemoryTaskStore) Delete(ctx context.Context, processID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[processID]; !exists {
		return ErrTaskNotFound
	}

	delete(s.tasks, processID)
	return nil
}

// Cleanup removes expired task results
func (s *InMemoryTaskStore) Cleanup(ctx context.Context, maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	for processID, result := range s.tasks {
		if result.CreatedAt.Before(cutoff) {
			delete(s.tasks, processID)
		}
	}

	return nil
}

// List returns all task results (for monitoring)
func (s *InMemoryTaskStore) List(ctx context.Context) ([]*TaskResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*TaskResult, 0, len(s.tasks))
	for _, result := range s.tasks {
		results = append(results, result)
	}

	return results, nil
}

// Common errors
var (
	ErrTaskNotFound = NewTaskError("task not found")
)

// TaskError represents a background task error
type TaskError struct {
	Message string
	Code    string
}

func NewTaskError(message string) *TaskError {
	return &TaskError{
		Message: message,
		Code:    "TASK_ERROR",
	}
}

func (e *TaskError) Error() string {
	return e.Message
}
