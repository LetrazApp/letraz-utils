package firecrawl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mendableai/firecrawl-go"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// FirecrawlScraper implements the Scraper interface using Firecrawl API
type FirecrawlScraper struct {
	config     *config.Config
	llmManager *llm.Manager
	app        *firecrawl.FirecrawlApp
	logger     types.Logger
}

// NewFirecrawlScraper creates a new Firecrawl scraper instance
func NewFirecrawlScraper(cfg *config.Config, llmManager *llm.Manager) *FirecrawlScraper {
	logger := logging.GetGlobalLogger()

	// Initialize Firecrawl app (only needs API key and API URL)
	app, err := firecrawl.NewFirecrawlApp(
		cfg.Firecrawl.APIKey,
		cfg.Firecrawl.APIURL,
	)
	if err != nil {
		logger.Error("Failed to initialize Firecrawl", map[string]interface{}{
			"error": err.Error(),
		})
		return nil
	}

	logger.Info("Firecrawl scraper initialized", map[string]interface{}{
		"api_url": cfg.Firecrawl.APIURL,
		"version": cfg.Firecrawl.Version,
	})

	return &FirecrawlScraper{
		config:     cfg,
		llmManager: llmManager,
		app:        app,
		logger:     logger,
	}
}

// ScrapeJob scrapes a job posting from the given URL using Firecrawl and LLM processing
func (f *FirecrawlScraper) ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error) {
	f.logger.Info("Starting Firecrawl job scraping", map[string]interface{}{
		"url": url,
	})

	// Scrape the URL using Firecrawl
	content, err := f.scrapeContent(ctx, url, options)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape content: %w", err)
	}

	// Check if LLM processing is disabled
	if options != nil && options.LLMProvider == "disabled" {
		return nil, fmt.Errorf("LLM processing is required for ScrapeJob but was disabled")
	}

	// Process the content with LLM to extract job information
	job, err := f.llmManager.ExtractJobData(ctx, content, url)
	if err != nil {
		// Don't wrap CustomError types so they can be properly handled upstream
		if _, ok := err.(*utils.CustomError); ok {
			return nil, err
		}
		return nil, fmt.Errorf("failed to parse job from content: %w", err)
	}

	f.logger.Info("Successfully scraped and parsed job", map[string]interface{}{
		"job_title": job.Title,
		"company":   job.CompanyName,
	})
	return job, nil
}

// ScrapeJobLegacy scrapes a job posting using legacy HTML parsing (returns basic extracted data)
func (f *FirecrawlScraper) ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error) {
	f.logger.Info("Starting Firecrawl legacy job scraping", map[string]interface{}{"url": url})

	// Scrape the URL using Firecrawl
	content, err := f.scrapeContent(ctx, url, options)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape content: %w", err)
	}

	// Create a basic job posting from scraped content
	jobPosting := &models.JobPosting{
		ID:             generateJobID(url),
		ApplicationURL: url,
		Description:    content,
		ProcessedAt:    time.Now(),
		Metadata: map[string]string{
			"scraper_engine": "firecrawl",
			"content_length": fmt.Sprintf("%d", len(content)),
		},
	}

	// Try to extract basic information without LLM if possible
	if title := extractSimpleTitle(content); title != "" {
		jobPosting.Title = title
	}

	f.logger.Info("Successfully scraped job posting (legacy mode)", map[string]interface{}{"url": url})
	return jobPosting, nil
}

// scrapeContent performs the actual Firecrawl scraping
func (f *FirecrawlScraper) scrapeContent(ctx context.Context, url string, options *models.ScrapeOptions) (string, error) {
	// Prepare scrape parameters
	scrapeParams := &firecrawl.ScrapeParams{
		Formats: f.config.Firecrawl.Formats,
	}

	// Note: Firecrawl Go SDK doesn't expose timeout in scrape params directly
	// Timeout control is handled internally by the SDK

	// Perform the scrape with retry logic
	var scrapeResult *firecrawl.FirecrawlDocument
	var err error

	for attempt := 1; attempt <= f.config.Firecrawl.MaxRetries; attempt++ {
		f.logger.Info("Firecrawl scrape attempt", map[string]interface{}{
			"attempt":     attempt,
			"max_retries": f.config.Firecrawl.MaxRetries,
			"url":         url,
		})

		scrapeResult, err = f.app.ScrapeURL(url, scrapeParams)
		if err == nil {
			break
		}

		f.logger.Info("Firecrawl scrape attempt failed", map[string]interface{}{
			"attempt": attempt,
			"error":   err.Error(),
		})

		if attempt < f.config.Firecrawl.MaxRetries {
			// Wait before retry
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	if err != nil {
		return "", fmt.Errorf("firecrawl scraping failed after %d attempts: %w", f.config.Firecrawl.MaxRetries, err)
	}

	if scrapeResult == nil {
		return "", fmt.Errorf("no result returned from Firecrawl")
	}

	// Extract content from the document
	var content string
	if scrapeResult.Markdown != "" {
		content = scrapeResult.Markdown
	} else if scrapeResult.HTML != "" {
		content = scrapeResult.HTML
	} else {
		return "", fmt.Errorf("no content found in Firecrawl response")
	}

	f.logger.Info("Successfully scraped content", map[string]interface{}{
		"content_length": len(content),
		"url":            url,
	})
	return content, nil
}

// Cleanup releases any resources used by the scraper
func (f *FirecrawlScraper) Cleanup() {
	f.logger.Info("Cleaning up Firecrawl scraper resources", nil)
	// Firecrawl SDK doesn't require explicit cleanup
	// Just log the cleanup
}

// IsHealthy checks if the scraper is healthy and ready to process requests
func (f *FirecrawlScraper) IsHealthy() bool {
	if f.app == nil {
		return false
	}

	if f.config.Firecrawl.APIKey == "" {
		f.logger.Info("Firecrawl API key not configured", nil)
		return false
	}

	// We could add a health check API call here if Firecrawl provides one
	// For now, we assume it's healthy if properly initialized
	return true
}

// Helper functions

// generateJobID creates a simple ID from URL for legacy job postings
func generateJobID(url string) string {
	// Simple hash-like ID generation
	return fmt.Sprintf("firecrawl_%d", time.Now().Unix())
}

// extractSimpleTitle attempts to extract a job title from content without LLM
func extractSimpleTitle(content string) string {
	// Simple heuristic: look for lines that might be titles
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 10 && len(line) < 100 {
			// Skip lines that look like navigation or boilerplate
			if !strings.Contains(strings.ToLower(line), "cookie") &&
				!strings.Contains(strings.ToLower(line), "privacy") &&
				!strings.Contains(strings.ToLower(line), "menu") &&
				!strings.Contains(strings.ToLower(line), "search") {
				return line
			}
		}
	}
	return ""
}
