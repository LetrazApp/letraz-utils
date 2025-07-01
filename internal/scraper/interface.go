package scraper

import (
	"context"

	"letraz-scrapper/pkg/models"
)

// Scraper defines the interface for job scraping engines
type Scraper interface {
	// ScrapeJob scrapes a job posting from the given URL using LLM processing
	ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error)

	// ScrapeJobLegacy scrapes a job posting using legacy HTML parsing (for backward compatibility)
	ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error)

	// Cleanup releases any resources used by the scraper
	Cleanup()

	// IsHealthy checks if the scraper is healthy and ready to process requests
	IsHealthy() bool
}

// ScraperFactory creates scrapers based on engine type
type ScraperFactory interface {
	// CreateScraper creates a new scraper instance for the given engine
	CreateScraper(engine string) (Scraper, error)

	// GetSupportedEngines returns a list of supported engine types
	GetSupportedEngines() []string
}
