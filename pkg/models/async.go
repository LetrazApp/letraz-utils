package models

import (
	"time"
)

// AsyncStatus represents the status of an async operation
type AsyncStatus string

const (
	AsyncStatusAccepted   AsyncStatus = "ACCEPTED"
	AsyncStatusProcessing AsyncStatus = "PROCESSING"
	AsyncStatusSuccess    AsyncStatus = "SUCCESS"
	AsyncStatusFailure    AsyncStatus = "FAILURE"
)

// AsyncScrapeResponse represents the immediate response from async scrape endpoint
type AsyncScrapeResponse struct {
	ProcessID string      `json:"processId"`
	Status    AsyncStatus `json:"status"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
}

// AsyncTailorResponse represents the immediate response from async tailor endpoint
type AsyncTailorResponse struct {
	ProcessID string      `json:"processId"`
	Status    AsyncStatus `json:"status"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
}

// AsyncTaskStatusResponse represents the response for task status queries
type AsyncTaskStatusResponse struct {
	ProcessID      string                 `json:"processId"`
	Status         AsyncStatus            `json:"status"`
	Data           interface{}            `json:"data,omitempty"`
	Error          string                 `json:"error,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	CompletedAt    *time.Time             `json:"completedAt,omitempty"`
	ProcessingTime *time.Duration         `json:"processingTime,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// AsyncScrapeCompletionData represents the completion data for scrape tasks
type AsyncScrapeCompletionData struct {
	Job        *Job        `json:"job,omitempty"`
	JobPosting *JobPosting `json:"job_posting,omitempty"`
	Engine     string      `json:"engine"`
	UsedLLM    bool        `json:"used_llm"`
}

// AsyncTailorCompletionData represents the completion data for tailor tasks
type AsyncTailorCompletionData struct {
	TailoredResume *TailoredResume `json:"tailored_resume,omitempty"`
	Suggestions    []Suggestion    `json:"suggestions,omitempty"`
	ThreadID       string          `json:"thread_id,omitempty"`
}

// AsyncTaskListResponse represents the response for listing tasks
type AsyncTaskListResponse struct {
	Success bool                      `json:"success"`
	Tasks   []AsyncTaskStatusResponse `json:"tasks"`
	Count   int                       `json:"count"`
}

// AsyncErrorResponse represents an error response for async operations
type AsyncErrorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message"`
	ProcessID string    `json:"processId,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// CreateAsyncScrapeResponse creates a successful async scrape response
func CreateAsyncScrapeResponse(processID string) *AsyncScrapeResponse {
	return &AsyncScrapeResponse{
		ProcessID: processID,
		Status:    AsyncStatusAccepted,
		Message:   "Scraping request accepted for background processing",
		Timestamp: time.Now(),
	}
}

// CreateAsyncTailorResponse creates a successful async tailor response
func CreateAsyncTailorResponse(processID string) *AsyncTailorResponse {
	return &AsyncTailorResponse{
		ProcessID: processID,
		Status:    AsyncStatusAccepted,
		Message:   "Resume tailoring request accepted for background processing",
		Timestamp: time.Now(),
	}
}

// CreateAsyncErrorResponse creates an error response for async operations
func CreateAsyncErrorResponse(error, message string, processID ...string) *AsyncErrorResponse {
	response := &AsyncErrorResponse{
		Error:     error,
		Message:   message,
		Timestamp: time.Now(),
	}

	if len(processID) > 0 && processID[0] != "" {
		response.ProcessID = processID[0]
	}

	return response
}

// IsCompleted checks if the async task has completed (success or failure)
func (r *AsyncTaskStatusResponse) IsCompleted() bool {
	return r.Status == AsyncStatusSuccess || r.Status == AsyncStatusFailure
}

// IsSuccessful checks if the async task completed successfully
func (r *AsyncTaskStatusResponse) IsSuccessful() bool {
	return r.Status == AsyncStatusSuccess
}

// IsFailed checks if the async task failed
func (r *AsyncTaskStatusResponse) IsFailed() bool {
	return r.Status == AsyncStatusFailure
}

// IsProcessing checks if the async task is currently processing
func (r *AsyncTaskStatusResponse) IsProcessing() bool {
	return r.Status == AsyncStatusProcessing
}

// IsAccepted checks if the async task has been accepted but not started
func (r *AsyncTaskStatusResponse) IsAccepted() bool {
	return r.Status == AsyncStatusAccepted
}

// GetProcessingDuration returns the processing duration if available
func (r *AsyncTaskStatusResponse) GetProcessingDuration() time.Duration {
	if r.ProcessingTime != nil {
		return *r.ProcessingTime
	}
	return 0
}

// GetCompletionTime returns the completion time if available
func (r *AsyncTaskStatusResponse) GetCompletionTime() *time.Time {
	return r.CompletedAt
}

// HasError checks if the response has an error
func (r *AsyncTaskStatusResponse) HasError() bool {
	return r.Error != ""
}

// GetScrapeData returns the scrape data if this is a scrape task
func (r *AsyncTaskStatusResponse) GetScrapeData() *AsyncScrapeCompletionData {
	if data, ok := r.Data.(*AsyncScrapeCompletionData); ok {
		return data
	}
	return nil
}

// GetTailorData returns the tailor data if this is a tailor task
func (r *AsyncTaskStatusResponse) GetTailorData() *AsyncTailorCompletionData {
	if data, ok := r.Data.(*AsyncTailorCompletionData); ok {
		return data
	}
	return nil
}
