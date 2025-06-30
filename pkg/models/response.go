package models

import "time"

// ScrapeResponse represents the response from a scraping operation
type ScrapeResponse struct {
	Success        bool          `json:"success"`
	Job            *JobPosting   `json:"job,omitempty"`
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
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
} 