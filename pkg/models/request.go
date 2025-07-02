package models

import "time"

// ScrapeRequest represents the request payload for scraping a job posting
type ScrapeRequest struct {
	URL     string         `json:"url" validate:"required,url"`
	Options *ScrapeOptions `json:"options,omitempty"`
}

// ScrapeOptions provides additional configuration for scraping requests
type ScrapeOptions struct {
	Engine      string        `json:"engine,omitempty"`       // "hybrid", "firecrawl", "headed", "rod", "auto"
	Timeout     time.Duration `json:"timeout,omitempty"`      // Request timeout
	LLMProvider string        `json:"llm_provider,omitempty"` // "claude", "disabled" (for legacy mode)
	UserAgent   string        `json:"user_agent,omitempty"`   // Custom user agent
	Proxy       string        `json:"proxy,omitempty"`        // Proxy configuration
}
