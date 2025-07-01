package models

import "time"

// ScrapeResponse represents the response from a scrape request
type ScrapeResponse struct {
	Success        bool          `json:"success"`
	Job            *Job          `json:"job,omitempty"`         // New job structure
	JobPosting     *JobPosting   `json:"job_posting,omitempty"` // Legacy structure for backward compatibility
	Error          string        `json:"error,omitempty"`
	ProcessingTime time.Duration `json:"processing_time"`
	Engine         string        `json:"engine_used"`
	RequestID      string        `json:"request_id"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Version   string            `json:"version"`
	Uptime    time.Duration     `json:"uptime"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
}
