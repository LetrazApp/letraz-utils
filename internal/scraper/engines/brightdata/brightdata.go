package brightdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// BrightDataScraper implements job scraping using BrightData API for LinkedIn
type BrightDataScraper struct {
	config     *config.Config
	llmManager *llm.Manager
	httpClient *http.Client
	logger     types.Logger
}

// BrightDataRequest represents the request structure for BrightData API
type BrightDataRequest struct {
	URL string `json:"url"`
}

// BrightDataResponse represents the response from BrightData API
type BrightDataResponse struct {
	Data []interface{} `json:"data,omitempty"`
	// BrightData returns varying JSON structures, so we'll handle it as interface{}
}

// NewBrightDataScraper creates a new BrightData scraper instance
func NewBrightDataScraper(cfg *config.Config, llmManager *llm.Manager) *BrightDataScraper {
	logger := logging.GetGlobalLogger()

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: cfg.BrightData.Timeout,
	}

	logger.Info("BrightData scraper initialized", map[string]interface{}{
		"base_url":   cfg.BrightData.BaseURL,
		"dataset_id": cfg.BrightData.DatasetID,
		"timeout":    cfg.BrightData.Timeout.String(),
	})

	return &BrightDataScraper{
		config:     cfg,
		llmManager: llmManager,
		httpClient: httpClient,
		logger:     logger,
	}
}

// ScrapeJob scrapes a LinkedIn job posting using BrightData API
func (bs *BrightDataScraper) ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error) {
	startTime := time.Now()

	bs.logger.Info("Starting LinkedIn job scrape with BrightData engine", map[string]interface{}{
		"url":    url,
		"engine": "brightdata",
	})

	// Validate that this is a LinkedIn URL
	if !utils.IsLinkedInURL(url) {
		return nil, utils.NewValidationError("BrightData engine only supports LinkedIn URLs")
	}

	// Parse and convert LinkedIn URL to public format if needed
	publicURL, err := utils.ConvertToPublicLinkedInJobURL(url)
	if err != nil {
		return nil, err // This will return NotJobPostingError for non-job LinkedIn URLs
	}

	// Extract job ID for logging
	jobID, _ := utils.ExtractLinkedInJobID(publicURL)

	bs.logger.Info("Processing LinkedIn job URL", map[string]interface{}{
		"original_url": url,
		"public_url":   publicURL,
		"job_id":       jobID,
	})

	// Call BrightData API
	jsonData, err := bs.callBrightDataAPI(ctx, publicURL)
	if err != nil {
		return nil, fmt.Errorf("BrightData API call failed: %w", err)
	}

	// Convert JSON response to string for LLM processing
	jsonString, err := json.Marshal(jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal BrightData response: %w", err)
	}

	bs.logger.Info("Received response from BrightData, sending to LLM for processing", map[string]interface{}{
		"response_size": len(jsonString),
		"job_id":        jobID,
	})

	// Use LLM to extract job information from JSON data
	job, err := bs.llmManager.ExtractJobData(ctx, string(jsonString), publicURL)
	if err != nil {
		// Don't wrap CustomError types so they can be properly handled upstream
		if customErr, ok := err.(*utils.CustomError); ok {
			return nil, customErr
		}
		return nil, fmt.Errorf("LLM job extraction failed: %w", err)
	}

	// Ensure the job URL is set correctly
	if job.JobURL == "" {
		job.JobURL = publicURL
	}

	processingTime := time.Since(startTime)
	bs.logger.Info("LinkedIn job scrape completed successfully", map[string]interface{}{
		"url":             publicURL,
		"job_id":          jobID,
		"title":           job.Title,
		"company":         job.CompanyName,
		"processing_time": processingTime.String(),
		"engine":          "brightdata",
	})

	return job, nil
}

// ScrapeJobLegacy provides backward compatibility (not used for BrightData)
func (bs *BrightDataScraper) ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error) {
	return nil, fmt.Errorf("legacy scraping not supported by BrightData engine")
}

// callBrightDataAPI makes the HTTP request to BrightData API
func (bs *BrightDataScraper) callBrightDataAPI(ctx context.Context, url string) (interface{}, error) {
	// Prepare request payload
	requestData := []BrightDataRequest{
		{URL: url},
	}

	jsonPayload, err := json.Marshal(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	// Build API URL
	apiURL := fmt.Sprintf("%s/datasets/v3/scrape?dataset_id=%s&include_errors=true",
		bs.config.BrightData.BaseURL,
		bs.config.BrightData.DatasetID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bs.config.BrightData.APIKey))

	bs.logger.Debug("Making BrightData API request", map[string]interface{}{
		"url":        url,
		"api_url":    apiURL,
		"dataset_id": bs.config.BrightData.DatasetID,
	})

	// Execute request with retry logic
	var lastErr error
	maxRetries := bs.config.BrightData.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			bs.logger.Debug("Retrying BrightData API request", map[string]interface{}{
				"attempt": attempt + 1,
				"url":     url,
			})

			// Exponential backoff
			backoffDelay := time.Duration(attempt) * time.Second
			time.Sleep(backoffDelay)
		}

		resp, err := bs.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			continue
		}
		defer resp.Body.Close()

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		// Check HTTP status
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("BrightData API returned status %d: %s", resp.StatusCode, string(body))

			// Don't retry on client errors (4xx)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				break
			}
			continue
		}

		// Parse JSON response
		var response interface{}
		if err := json.Unmarshal(body, &response); err != nil {
			lastErr = fmt.Errorf("failed to parse JSON response: %w", err)
			continue
		}

		bs.logger.Info("BrightData API request successful", map[string]interface{}{
			"url":           url,
			"response_size": len(body),
			"status_code":   resp.StatusCode,
		})

		return response, nil
	}

	return nil, fmt.Errorf("BrightData API failed after %d attempts: %v", maxRetries+1, lastErr)
}

// Cleanup releases any resources used by the scraper
func (bs *BrightDataScraper) Cleanup() {
	// Close HTTP client if needed
	if bs.httpClient != nil {
		bs.httpClient.CloseIdleConnections()
	}

	bs.logger.Debug("BrightData scraper cleanup completed", map[string]interface{}{})
}

// IsHealthy checks if the BrightData scraper is healthy and ready to process requests
func (bs *BrightDataScraper) IsHealthy() bool {
	// Check if API key is configured
	if bs.config.BrightData.APIKey == "" {
		bs.logger.Debug("BrightData health check failed: no API key configured", nil)
		return false
	}

	// Check if base URL and dataset ID are configured
	if bs.config.BrightData.BaseURL == "" || bs.config.BrightData.DatasetID == "" {
		bs.logger.Debug("BrightData health check failed: missing configuration", map[string]interface{}{
			"base_url":   bs.config.BrightData.BaseURL,
			"dataset_id": bs.config.BrightData.DatasetID,
		})
		return false
	}

	// Simple connectivity test could be added here, but for now we'll just check config
	bs.logger.Debug("BrightData health check successful", map[string]interface{}{
		"base_url":   bs.config.BrightData.BaseURL,
		"dataset_id": bs.config.BrightData.DatasetID,
	})

	return true
}
