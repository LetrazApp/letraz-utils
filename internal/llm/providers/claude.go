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

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/llm/processors"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"
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
		return nil, fmt.Errorf("failed to call Claude API: %w", err)
	}

	// Parse the response
	job, err := cp.parseClaudeResponse(response, url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	processingTime := time.Since(startTime)
	cp.logger.WithFields(logrus.Fields{
		"url":             url,
		"job_title":       job.Title,
		"company":         job.CompanyName,
		"processing_time": processingTime,
		"provider":        "claude",
	}).Info("Job data extraction completed successfully")

	return job, nil
}

// buildJobExtractionPrompt creates the prompt for Claude to extract job data
func (cp *ClaudeProvider) buildJobExtractionPrompt(content, url string) string {
	return fmt.Sprintf(`You are a job posting analyzer. Extract structured job information from the provided content and return it as a JSON object.

The content below is from a job posting webpage. Please extract the following information and return it as a valid JSON object with exactly these fields:

{
  "title": "string - The job title",
  "job_url": "string - The URL of the job posting (%s)",
  "company_name": "string - The company name",
  "location": "string - The job location (city, state, country, or 'Remote')",
  "salary": {
    "currency": "string - Salary as displayed (e.g., '$80,000 - $100,000 per year')",
    "max": number - Maximum salary as integer (0 if not specified),
    "min": number - Minimum salary as integer (0 if not specified)
  },
  "requirements": ["array of strings - Required qualifications, skills, experience"],
  "description": "string - Brief job description or summary (2-3 sentences max)",
  "responsibilities": ["array of strings - Key job responsibilities and duties"],
  "benefits": ["array of strings - Employee benefits, perks, compensation details"]
}

IMPORTANT RULES:
1. Return ONLY valid JSON, no additional text or explanation
2. If information is not found, use empty string "" for strings, empty array [] for arrays, and 0 for numbers
3. For salary: extract any monetary values mentioned (annual, hourly, etc.)
4. Keep descriptions concise but informative
5. Extract responsibilities and requirements separately
6. Include the provided URL as job_url
7. If the content doesn't appear to be a job posting, return a JSON with empty/null values but maintain the structure

JOB POSTING CONTENT:
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

	// Parse JSON response
	var job models.Job
	if err := json.Unmarshal([]byte(responseText), &job); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response from Claude: %w, response: %s", err, responseText)
	}

	// Ensure job_url is set correctly
	if job.JobURL == "" {
		job.JobURL = url
	}

	// Validate required fields
	if job.Title == "" {
		job.Title = "Title Not Found"
	}
	if job.CompanyName == "" {
		job.CompanyName = "Company Not Found"
	}

	return &job, nil
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
