package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/sirupsen/logrus"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm/processors"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// ClaudeProvider implements the LLM provider interface using Anthropic's Claude
type ClaudeProvider struct {
	client      anthropic.Client
	config      *config.Config
	htmlCleaner *processors.HTMLCleaner
	logger      *logrus.Logger
}

// NewClaudeProvider creates a new Claude provider instance
func NewClaudeProvider(cfg *config.Config) *ClaudeProvider {
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.LLM.APIKey),
	)

	return &ClaudeProvider{
		client:      client,
		config:      cfg,
		htmlCleaner: processors.NewHTMLCleaner(),
		logger:      utils.GetLogger(),
	}
}

// ExtractJobData processes HTML content and extracts structured job data using Claude
func (cp *ClaudeProvider) ExtractJobData(ctx context.Context, html, url string) (*models.Job, error) {
	startTime := time.Now()

	cp.logger.WithFields(logrus.Fields{
		"url":         url,
		"html_length": len(html),
		"provider":    "claude",
	}).Info("Starting job data extraction with Claude")

	// Clean and preprocess HTML
	cleanedContent, err := cp.htmlCleaner.ExtractJobContent(html)
	if err != nil {
		return nil, fmt.Errorf("failed to clean HTML: %w", err)
	}

	// Check content length and truncate if necessary to fit token limits
	maxContentLength := cp.config.LLM.MaxTokens * 3 // Rough estimation: 3 chars per token
	if len(cleanedContent) > maxContentLength {
		cleanedContent = cleanedContent[:maxContentLength] + "..."
		cp.logger.WithField("url", url).Debug("Content truncated to fit token limits")
	}

	// Create the prompt for Claude
	prompt := cp.buildJobExtractionPrompt(cleanedContent, url)

	// Make request to Claude
	response, err := cp.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.ModelClaude3_7SonnetLatest,
		MaxTokens:   int64(cp.config.LLM.MaxTokens),
		Temperature: anthropic.Float(float64(cp.config.LLM.Temperature)),
		Messages: []anthropic.MessageParam{{
			Content: []anthropic.ContentBlockParamUnion{{
				OfText: &anthropic.TextBlockParam{Text: prompt},
			}},
			Role: anthropic.MessageParamRoleUser,
		}},
	})

	if err != nil {
		cp.logger.WithFields(logrus.Fields{
			"url":      url,
			"provider": "claude",
			"error":    err.Error(),
		}).Error("Claude API call failed")
		return nil, fmt.Errorf("failed to call Claude API: %w", err)
	}

	cp.logger.WithFields(logrus.Fields{
		"url":      url,
		"provider": "claude",
	}).Debug("Claude API call successful, parsing response")

	// Parse the response
	job, err := cp.parseClaudeResponse(response, url)
	if err != nil {
		cp.logger.WithFields(logrus.Fields{
			"url":      url,
			"provider": "claude",
			"error":    err.Error(),
		}).Error("Failed to parse Claude response")

		// Don't wrap CustomError types so they can be properly handled upstream
		if _, ok := err.(*utils.CustomError); ok {
			return nil, err
		}

		return nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	processingTime := time.Since(startTime)
	cp.logger.WithFields(logrus.Fields{
		"url":             url,
		"processing_time": processingTime,
		"provider":        "claude",
	}).Info("Job data extraction completed successfully")

	return job, nil
}

// buildJobExtractionPrompt creates the prompt for Claude to extract job data
func (cp *ClaudeProvider) buildJobExtractionPrompt(content, url string) string {
	return fmt.Sprintf(`You are a job posting analyzer. Analyze the provided content to determine if it contains a job posting, and if so, extract structured job information.

The content below is from a webpage. Please first determine if this is actually a job posting, then extract information accordingly.

Return a JSON object with exactly these fields:

{
  "is_job_posting": boolean - true if this content contains a job posting, false otherwise,
  "confidence": number - confidence score from 0.0 to 1.0 (only if is_job_posting is true),
  "title": "string - The job title (empty if not a job posting)",
  "job_url": "string - The URL of the job posting (%s)",
  "company_name": "string - The company name (empty if not a job posting)",
  "location": "string - The job location (city, state, country, or 'Remote')",
  "salary": {
    "currency": "string - The currency salary is being mentioned in (e.g., 'USD' or 'INR')",
    "max": number - Maximum salary as integer (0 if not specified),
    "min": number - Minimum salary as integer (0 if not specified)
  },
  "requirements": ["array of strings - Required qualifications, skills, experience"],
  "description": "string - Brief job description or summary (2-3 sentences max)",
  "responsibilities": ["array of strings - Key job responsibilities and duties"],
  "benefits": ["array of strings - Employee benefits, perks, compensation details"],
  "reason": "string - Brief explanation if not a job posting (e.g., 'This appears to be a company homepage', 'This is a news article')"
}

IMPORTANT CLASSIFICATION RULES:
1. A job posting should contain:
   - A specific job title/position
   - Job responsibilities or description
   - Company information
   - Usually requirements or qualifications
   
2. NOT job postings include:
   - Company homepages or about pages
   - News articles or blog posts
   - Product pages or marketing content
   - Search results or listing pages
   - Error pages or redirects
   - General career pages without specific positions

EXTRACTION RULES:
- Return ONLY valid JSON, no additional text or explanation
- If is_job_posting is false, fill title, company_name, and other job fields with empty strings/arrays
- If is_job_posting is true, extract all available information
- For salary: extract any monetary values mentioned (annual, hourly, etc.)
- Keep descriptions concise but informative
- Set confidence to at least 0.7 for clear job postings, lower for ambiguous content

CONTENT TO ANALYZE:
%s`, url, content)
}

// parseClaudeResponse parses the Claude API response and extracts the job data
func (cp *ClaudeProvider) parseClaudeResponse(response *anthropic.Message, url string) (*models.Job, error) {
	if len(response.Content) == 0 {
		return nil, fmt.Errorf("empty response from Claude")
	}

	// Get the text content from the response
	var responseText string
	for _, content := range response.Content {
		textContent := content.AsText()
		responseText = textContent.Text
		break
	}

	if responseText == "" {
		return nil, fmt.Errorf("no text content in Claude response")
	}

	// Clean the response - remove any markdown code blocks if present
	responseText = strings.TrimSpace(responseText)
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	}

	cp.logger.WithField("response_text", responseText).Debug("Claude response received")

	// Parse JSON response with validation fields
	var rawResponse struct {
		IsJobPosting     bool          `json:"is_job_posting"`
		Confidence       float64       `json:"confidence"`
		Title            string        `json:"title"`
		JobURL           string        `json:"job_url"`
		CompanyName      string        `json:"company_name"`
		Location         string        `json:"location"`
		Salary           models.Salary `json:"salary"`
		Requirements     []string      `json:"requirements"`
		Description      string        `json:"description"`
		Responsibilities []string      `json:"responsibilities"`
		Benefits         []string      `json:"benefits"`
		Reason           string        `json:"reason"`
	}

	if err := json.Unmarshal([]byte(responseText), &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response from Claude: %w, response: %s", err, responseText)
	}

	// Check if the content is actually a job posting
	if !rawResponse.IsJobPosting {
		reason := rawResponse.Reason
		if reason == "" {
			reason = "The provided URL does not contain a job posting"
		}
		return nil, utils.NewNotJobPostingError(fmt.Sprintf("URL '%s' is not a job posting: %s", url, reason))
	}

	// Check confidence level for job postings
	if rawResponse.Confidence < 0.7 {
		return nil, utils.NewNotJobPostingError(fmt.Sprintf("Low confidence (%.2f) that URL '%s' contains a valid job posting", rawResponse.Confidence, url))
	}

	// Create job object from validated response
	job := &models.Job{
		Title:            rawResponse.Title,
		JobURL:           rawResponse.JobURL,
		CompanyName:      rawResponse.CompanyName,
		Location:         rawResponse.Location,
		Salary:           rawResponse.Salary,
		Requirements:     rawResponse.Requirements,
		Description:      rawResponse.Description,
		Responsibilities: rawResponse.Responsibilities,
		Benefits:         rawResponse.Benefits,
	}

	// Ensure job_url is set correctly
	if job.JobURL == "" {
		job.JobURL = url
	}

	// Validate required fields for confirmed job postings
	if job.Title == "" {
		return nil, utils.NewNotJobPostingError(fmt.Sprintf("No job title found in URL '%s' - content may not be a valid job posting", url))
	}
	if job.CompanyName == "" {
		return nil, utils.NewNotJobPostingError(fmt.Sprintf("No company name found in URL '%s' - content may not be a valid job posting", url))
	}

	cp.logger.Info("Successfully validated and extracted job posting")

	return job, nil
}

// IsHealthy checks if the Claude provider is healthy and available
func (cp *ClaudeProvider) IsHealthy(ctx context.Context) error {
	// Check if API key is configured
	if cp.config.LLM.APIKey == "" {
		return fmt.Errorf("Claude API key not configured - set LLM_API_KEY environment variable")
	}

	// Create a simple test request to check if the API is accessible
	_, err := cp.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_7SonnetLatest,
		MaxTokens: 500,
		Messages: []anthropic.MessageParam{{
			Content: []anthropic.ContentBlockParamUnion{{
				OfText: &anthropic.TextBlockParam{Text: "Hello"},
			}},
			Role: anthropic.MessageParamRoleUser,
		}},
	})

	if err != nil {
		return fmt.Errorf("Claude API health check failed: %w", err)
	}

	return nil
}

// GetProviderName returns the name of the LLM provider
func (cp *ClaudeProvider) GetProviderName() string {
	return "claude"
}
