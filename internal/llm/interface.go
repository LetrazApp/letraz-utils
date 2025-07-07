package llm

import (
	"context"
	"letraz-utils/pkg/models"
)

// LLMProvider defines the interface for LLM providers
type LLMProvider interface {
	// ExtractJobData processes HTML content and extracts structured job data
	ExtractJobData(ctx context.Context, html, url string) (*models.Job, error)

	// IsHealthy checks if the LLM provider is healthy and available
	IsHealthy(ctx context.Context) error

	// GetProviderName returns the name of the LLM provider
	GetProviderName() string
}

// ExtractJobDataRequest represents the request to extract job data
type ExtractJobDataRequest struct {
	HTML string `json:"html"`
	URL  string `json:"url"`
}

// ExtractJobDataResponse represents the response from job data extraction
type ExtractJobDataResponse struct {
	Job   *models.Job `json:"job"`
	Error string      `json:"error,omitempty"`
}
