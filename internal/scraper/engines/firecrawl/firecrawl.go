package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	// Try Firecrawl extract first if enabled
	f.logger.Info("Checking Firecrawl extract configuration", map[string]interface{}{
		"use_extract": f.config.Firecrawl.UseExtract,
	})

	if f.config.Firecrawl.UseExtract {
		f.logger.Info("Attempting Firecrawl extract with schema", map[string]interface{}{
			"url": url,
		})
		job, err := f.extractJobWithFirecrawl(ctx, url)
		if err == nil && job != nil {
			f.logger.Info("Firecrawl extract succeeded", map[string]interface{}{
				"url":       url,
				"job_title": job.Title,
				"company":   job.CompanyName,
				"path":      "extract",
			})
			return job, nil
		}
		if err != nil {
			f.logger.Warn("Firecrawl extract failed; falling back to scrape + LLM", map[string]interface{}{
				"url":   url,
				"error": err.Error(),
			})
		} else {
			f.logger.Warn("Firecrawl extract returned empty result; falling back to scrape + LLM", map[string]interface{}{
				"url": url,
			})
		}
	}

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

// extractJobWithFirecrawl calls Firecrawl's extract API with a JSON schema and maps the response to models.Job
func (f *FirecrawlScraper) extractJobWithFirecrawl(ctx context.Context, url string) (*models.Job, error) {
	// Build endpoint: always use v2 for schema-based extraction
	base := strings.TrimRight(f.config.Firecrawl.APIURL, "/")
	endpoint := base + "/v2/scrape"

	payload := map[string]interface{}{
		"url":             url,
		"onlyMainContent": true,
		"maxAge":          172800000,       // Match the working cURL
		"parsers":         []string{"pdf"}, // Match the working cURL
		"formats": []map[string]interface{}{
			{
				"type":   "json",
				"schema": f.getJobExtractionSchema(),
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)

	f.logger.Info("Sending Firecrawl v2/scrape request", map[string]interface{}{
		"endpoint":     endpoint,
		"url":          url,
		"payload_size": len(bodyBytes),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create extract request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if f.config.Firecrawl.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.config.Firecrawl.APIKey)
	}

	httpClient := &http.Client{Timeout: f.config.Firecrawl.Timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extract request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	f.logger.Debug("Received Firecrawl response", map[string]interface{}{
		"status_code":   resp.StatusCode,
		"response_size": len(respBody),
		"content_type":  resp.Header.Get("Content-Type"),
	})

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		f.logger.Warn("Firecrawl extract failed", map[string]interface{}{
			"status_code": resp.StatusCode,
			"endpoint":    endpoint,
		})
		f.logger.Debug("Firecrawl extract error details", map[string]interface{}{
			"response_body": truncateForLog(string(respBody), 1000),
		})
		return nil, fmt.Errorf("extract request returned status %d", resp.StatusCode)
	}

	// Parse response and attempt to locate the job object
	var root interface{}
	if err := json.Unmarshal(respBody, &root); err != nil {
		return nil, fmt.Errorf("failed to parse extract response: %w", err)
	}

	f.logger.Debug("Parsed Firecrawl response, looking for job object", map[string]interface{}{
		"response_type": fmt.Sprintf("%T", root),
	})

	match := findJobObjectRecursive(root)
	if match == nil {
		// Try common wrappers as a fallback
		if m, ok := root.(map[string]interface{}); ok {
			if v, ok := m["data"]; ok {
				match = findJobObjectRecursive(v)
			} else if v, ok := m["result"]; ok {
				match = findJobObjectRecursive(v)
			}
		}
	}

	if match == nil {
		f.logger.Warn("Could not find job object in response", map[string]interface{}{
			"response_type": fmt.Sprintf("%T", root),
		})
		f.logger.Debug("Response details for missing job object", map[string]interface{}{
			"response_sample": truncateForLog(string(respBody), 500),
		})
		return nil, fmt.Errorf("extract response did not contain a matching job object")
	}

	f.logger.Debug("Found job object in response", map[string]interface{}{
		"job_keys": getMapKeys(match),
	})

	objBytes, _ := json.Marshal(match)
	var job models.Job
	if err := json.Unmarshal(objBytes, &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal extracted job: %w", err)
	}

	if job.JobURL == "" {
		job.JobURL = url
	}

	if err := f.validateExtractedJob(job); err != nil {
		return nil, err
	}

	return &job, nil
}

// findJobObjectRecursive walks arbitrary JSON and returns the first map with keys matching our job schema
func findJobObjectRecursive(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		// Heuristic: require at least title and company_name
		if _, hasTitle := t["title"]; hasTitle {
			if _, hasCompany := t["company_name"]; hasCompany {
				return t
			}
		}
		for _, child := range t {
			if m := findJobObjectRecursive(child); m != nil {
				return m
			}
		}
	case []interface{}:
		for _, item := range t {
			if m := findJobObjectRecursive(item); m != nil {
				return m
			}
		}
	}
	return nil
}

func (f *FirecrawlScraper) validateExtractedJob(job models.Job) error {
	if strings.TrimSpace(job.Title) == "" {
		return utils.NewNotJobPostingError("extracted job missing title")
	}
	if strings.TrimSpace(job.CompanyName) == "" {
		return utils.NewNotJobPostingError("extracted job missing company_name")
	}
	return nil
}

func (f *FirecrawlScraper) getJobExtractionSchema() map[string]interface{} {
	var schema map[string]interface{}
	_ = json.Unmarshal([]byte(jobExtractionSchema), &schema)
	return schema
}

// truncateForLog safely truncates long payloads for logging
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// getMapKeys returns the keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

const jobExtractionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "company_name"],
  "properties": {
    "title": { "type": "string" },
    "job_url": { "type": "string", "format": "uri" },
    "company_name": { "type": "string" },
    "location": { "type": "string" },
    "salary": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "currency": { "type": "string" },
        "min": { "type": "number" },
        "max": { "type": "number" }
      }
    },
    "requirements": { "type": "array", "items": { "type": "string" } },
    "description": { "type": "string" },
    "responsibilities": { "type": "array", "items": { "type": "string" } },
    "benefits": { "type": "array", "items": { "type": "string" } }
  }
}`
