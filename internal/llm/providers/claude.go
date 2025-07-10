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

// TailorResume tailors a base resume for a specific job posting using Claude
func (cp *ClaudeProvider) TailorResume(ctx context.Context, baseResume *models.BaseResume, job *models.Job) (*models.TailoredResume, []models.Suggestion, error) {
	startTime := time.Now()

	cp.logger.WithFields(logrus.Fields{
		"resume_id": baseResume.ID,
		"job_title": job.Title,
		"company":   job.CompanyName,
		"provider":  "claude",
	}).Info("Starting resume tailoring with Claude")

	// Create the comprehensive prompt for resume tailoring
	prompt := cp.buildResumeTailoringPrompt(baseResume, job)

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
			"resume_id": baseResume.ID,
			"provider":  "claude",
			"error":     err.Error(),
		}).Error("Claude API call failed for resume tailoring")
		return nil, nil, fmt.Errorf("failed to call Claude API for resume tailoring: %w", err)
	}

	cp.logger.WithFields(logrus.Fields{
		"resume_id": baseResume.ID,
		"provider":  "claude",
	}).Debug("Claude API call successful for resume tailoring, parsing response")

	// Parse the response
	tailoredResume, suggestions, err := cp.parseResumeTailoringResponse(response, baseResume, job)
	if err != nil {
		cp.logger.WithFields(logrus.Fields{
			"resume_id": baseResume.ID,
			"provider":  "claude",
			"error":     err.Error(),
		}).Error("Failed to parse Claude resume tailoring response")
		return nil, nil, fmt.Errorf("failed to parse Claude resume tailoring response: %w", err)
	}

	processingTime := time.Since(startTime)
	cp.logger.WithFields(logrus.Fields{
		"resume_id":         baseResume.ID,
		"processing_time":   processingTime,
		"provider":          "claude",
		"suggestions_count": len(suggestions),
	}).Info("Resume tailoring completed successfully")

	return tailoredResume, suggestions, nil
}

// TailorResumeWithRawResponse tailors a resume and returns the raw AI response for conversation history
func (cp *ClaudeProvider) TailorResumeWithRawResponse(ctx context.Context, baseResume *models.BaseResume, job *models.Job) (*models.TailoredResume, []models.Suggestion, string, error) {
	startTime := time.Now()

	cp.logger.WithFields(logrus.Fields{
		"resume_id": baseResume.ID,
		"job_title": job.Title,
		"company":   job.CompanyName,
		"provider":  "claude",
	}).Info("Starting resume tailoring with Claude (with raw response)")

	// Create the comprehensive prompt for resume tailoring
	prompt := cp.buildResumeTailoringPrompt(baseResume, job)

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
			"resume_id": baseResume.ID,
			"provider":  "claude",
			"error":     err.Error(),
		}).Error("Claude API call failed for resume tailoring")
		return nil, nil, "", fmt.Errorf("failed to call Claude API for resume tailoring: %w", err)
	}

	cp.logger.WithFields(logrus.Fields{
		"resume_id": baseResume.ID,
		"provider":  "claude",
	}).Debug("Claude API call successful for resume tailoring, parsing response")

	// Get the raw response text first
	var rawResponse string
	if len(response.Content) > 0 {
		textContent := response.Content[0].AsText()
		rawResponse = textContent.Text
	}

	// Parse the response
	tailoredResume, suggestions, err := cp.parseResumeTailoringResponse(response, baseResume, job)
	if err != nil {
		cp.logger.WithFields(logrus.Fields{
			"resume_id": baseResume.ID,
			"provider":  "claude",
			"error":     err.Error(),
		}).Error("Failed to parse Claude resume tailoring response")
		return nil, nil, rawResponse, fmt.Errorf("failed to parse Claude resume tailoring response: %w", err)
	}

	processingTime := time.Since(startTime)
	cp.logger.WithFields(logrus.Fields{
		"resume_id":         baseResume.ID,
		"processing_time":   processingTime,
		"provider":          "claude",
		"suggestions_count": len(suggestions),
	}).Info("Resume tailoring with raw response completed successfully")

	return tailoredResume, suggestions, rawResponse, nil
}

// buildResumeTailoringPrompt creates the comprehensive prompt for Claude to tailor the resume
func (cp *ClaudeProvider) buildResumeTailoringPrompt(baseResume *models.BaseResume, job *models.Job) string {
	// Convert baseResume to JSON for the prompt
	resumeJSON, _ := json.MarshalIndent(baseResume, "", "  ")
	jobJSON, _ := json.MarshalIndent(job, "", "  ")

	return fmt.Sprintf(`You are an expert resume optimization specialist with years of experience helping professionals tailor their resumes for specific job applications. Your task is to analyze the provided base resume and job posting, then create a tailored version that maximizes the candidate's chances of success.

**CRITICAL INSTRUCTION - NO HALLUCINATIONS:**
- Use ONLY information that is directly provided in the base resume
- Do NOT add skills, experiences, technologies, or achievements not mentioned in the original resume
- Do NOT infer or assume qualifications beyond what is explicitly stated
- Do NOT add company names, project names, or specific details not in the original data
- You may REFRAME and EMPHASIZE existing information to align with job requirements
- You may use synonyms or industry-standard terms for existing skills/technologies
- If the resume lacks alignment with job requirements, note this in suggestions rather than fabricating missing elements

**BASE RESUME:**
%s

**TARGET JOB POSTING:**
%s

**YOUR TASK:**
1. **ANALYZE**: Carefully study both the resume and job posting to understand:
   - Key requirements and qualifications the employer is seeking
   - Skills, technologies, and experiences mentioned in the job description
   - Company culture and values (if evident)
   - Priority areas where the candidate's experience aligns with provided resume data

2. **TAILOR**: Optimize the resume content to align with the job requirements using ONLY existing information:
   - Rewrite experience descriptions to emphasize relevant achievements already mentioned
   - Highlight skills and technologies that match job requirements (only if already in resume)
   - Quantify accomplishments where numbers are already provided
   - Use keywords and terminology from the job posting naturally to describe existing experience
   - Adjust the professional summary/profile text to reflect the target role using existing background
   - Maintain truthfulness - never fabricate experience, skills, or specific details

3. **IMPROVE**: Enhance the overall quality and impact using only existing content:
   - Use strong action verbs and result-oriented language for existing accomplishments
   - Remove or de-emphasize less relevant experiences already in the resume
   - Improve clarity and readability of existing descriptions
   - Ensure consistency in formatting and style

**RESPONSE FORMAT:**
Return a JSON object with exactly this structure:

{
  "tailored_resume": {
    "id": "string - same as input resume ID",
    "base": false,
    "user": {
      // Keep user information exactly the same, but update profile_text to be tailored for this specific job using only existing background
      "profile_text": "string - Rewritten professional summary optimized for the target job using only existing experience/skills (2-3 sentences, HTML format like the original)"
    },
    "sections": [
      // Array of resume sections with tailored content
      // For Experience sections: rewrite descriptions to emphasize job-relevant achievements using only existing information
      // For Education sections: highlight relevant coursework or projects only if already mentioned
      // Maintain the exact same structure as the input resume
    ]
  },
  "suggestions": [
    {
      "id": "sug_001",
      "type": "experience",
      "priority": "high",
      "impact": "Emphasizing Python and Django skills would directly align with the job requirements and increase selection chances by 40%",
      "section": "Experience",
      "current": "Developed web applications using various technologies",
      "suggested": "Add specific mention of Python frameworks and API development experience in the experience descriptions",
      "reasoning": "The job specifically requires Python and Django expertise, which matches the candidate's background"
    },
    {
      "id": "sug_002",
      "type": "skills",
      "priority": "high",
      "impact": "Adding a dedicated skills section would immediately show job requirement alignment and improve screening chances",
      "section": "Skills",
      "current": "No dedicated skills section present",
      "suggested": "Create a skills section highlighting Python, Django, REST APIs, and database management",
      "reasoning": "Job posting emphasizes technical skills and having them prominently displayed would match ATS requirements"
    },
    {
      "id": "sug_003",
      "type": "profile",
      "priority": "medium",
      "impact": "Quantifying achievements with metrics would strengthen the profile and demonstrate measurable impact",
      "section": "Profile",
      "current": "Generic statements about experience",
      "suggested": "Include specific metrics from existing projects (e.g., 'improved system performance by X%', 'handled Y requests per day')",
      "reasoning": "Quantified achievements are more compelling to hiring managers and show concrete value delivery"
    }
  ]
}

**CRITICAL: SUGGESTIONS MUST BE OBJECTS, NOT STRINGS**
- Each suggestion MUST be a JSON object with all fields: id, type, priority, impact, section, current, suggested, reasoning
- DO NOT return suggestions as an array of strings like ["suggestion 1", "suggestion 2"]
- Return EXACTLY 3 suggestions, no more, no less
- Each suggestion must have meaningful, specific content for all fields

**EXAMPLE WRONG FORMAT (DO NOT USE):**
"suggestions": [
  "Add more technical skills",
  "Improve experience descriptions",
  "Quantify achievements"
]

**EXAMPLE CORRECT FORMAT (USE THIS):**
"suggestions": [
  {
    "id": "sug_001",
    "type": "experience",
    "priority": "high",
    "impact": "Specific description of how this increases job selection chances",
    "section": "Experience",
    "current": "Current state of the content",
    "suggested": "Specific actionable improvement",
    "reasoning": "Why this change helps for this specific job"
  }
]

**SUGGESTION GUIDELINES:**
- Limit to EXACTLY 3 suggestions maximum
- Focus on changes that would have the highest impact on job selection for this specific role
- Prioritize suggestions that address clear gaps between the resume and job requirements
- Be specific and actionable - avoid generic advice
- Consider which changes would make the biggest difference to a hiring manager for this role
- Think from the perspective: "If implemented, which 3 changes would most increase the chances of this resume being selected?"

**IMPORTANT GUIDELINES:**
- Keep the exact same structure and format as the input resume
- Only modify content, not structure or data types
- Preserve all IDs, timestamps, and metadata
- Focus on relevance while maintaining authenticity and not adding fabricated information
- Use HTML formatting in descriptions where the original uses it
- Suggestions should be specific and actionable, not generic advice
- Never suggest adding information that wasn't in the original resume

Return ONLY the JSON response, no additional text or explanations.`, string(resumeJSON), string(jobJSON))
}

// parseResumeTailoringResponse parses Claude's response for resume tailoring
func (cp *ClaudeProvider) parseResumeTailoringResponse(response *anthropic.Message, baseResume *models.BaseResume, job *models.Job) (*models.TailoredResume, []models.Suggestion, error) {
	if len(response.Content) == 0 {
		return nil, nil, fmt.Errorf("empty response from Claude")
	}

	// Get the text content from the response
	var responseText string
	for _, content := range response.Content {
		textContent := content.AsText()
		responseText = textContent.Text
		break
	}

	if responseText == "" {
		return nil, nil, fmt.Errorf("no text content in Claude response")
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

	cp.logger.WithField("response_length", len(responseText)).Debug("Claude resume tailoring response received")

	// Log the actual response for debugging
	cp.logger.WithField("raw_response", responseText).Debug("Raw Claude response for debugging")

	// Parse JSON response
	var tailoringResponse struct {
		TailoredResume models.TailoredResume `json:"tailored_resume"`
		Suggestions    []models.Suggestion   `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(responseText), &tailoringResponse); err != nil {
		// Try to parse as old format with string suggestions as fallback
		cp.logger.WithField("parse_error", err.Error()).Warn("Failed to parse structured suggestions, trying fallback")

		var fallbackResponse struct {
			TailoredResume models.TailoredResume `json:"tailored_resume"`
			Suggestions    []string              `json:"suggestions"`
		}

		if fallbackErr := json.Unmarshal([]byte(responseText), &fallbackResponse); fallbackErr != nil {
			return nil, nil, fmt.Errorf("failed to parse JSON response from Claude (both formats): primary error: %w, fallback error: %v, response: %s", err, fallbackErr, responseText)
		}

		// Convert string suggestions to structured format
		structuredSuggestions := make([]models.Suggestion, 0)
		maxSuggestions := 3
		if len(fallbackResponse.Suggestions) < maxSuggestions {
			maxSuggestions = len(fallbackResponse.Suggestions)
		}

		for i := 0; i < maxSuggestions; i++ {
			structuredSuggestions = append(structuredSuggestions, models.Suggestion{
				ID:        fmt.Sprintf("sug_%03d", i+1),
				Type:      "general",
				Priority:  "high",
				Impact:    "This change would improve resume alignment with job requirements",
				Section:   "general",
				Current:   "",
				Suggested: fallbackResponse.Suggestions[i],
				Reasoning: "Legacy suggestion format - manual review recommended",
			})
		}

		tailoringResponse.TailoredResume = fallbackResponse.TailoredResume
		tailoringResponse.Suggestions = structuredSuggestions

		cp.logger.Warn("Converted legacy string suggestions to structured format")
	}

	// Validate the response
	if tailoringResponse.TailoredResume.ID == "" {
		return nil, nil, fmt.Errorf("invalid tailored resume: missing ID")
	}

	if len(tailoringResponse.Suggestions) == 0 {
		return nil, nil, fmt.Errorf("invalid response: no suggestions provided")
	}

	// Validate that we have exactly 3 suggestions with required fields
	if len(tailoringResponse.Suggestions) > 3 {
		tailoringResponse.Suggestions = tailoringResponse.Suggestions[:3] // Limit to 3
	}

	for i, suggestion := range tailoringResponse.Suggestions {
		if suggestion.ID == "" {
			tailoringResponse.Suggestions[i].ID = fmt.Sprintf("sug_%03d", i+1)
		}
		if suggestion.Type == "" {
			return nil, nil, fmt.Errorf("invalid suggestion %d: missing type", i+1)
		}
		if suggestion.Impact == "" {
			return nil, nil, fmt.Errorf("invalid suggestion %d: missing impact description", i+1)
		}
		if suggestion.Suggested == "" {
			return nil, nil, fmt.Errorf("invalid suggestion %d: missing suggested improvement", i+1)
		}
		if suggestion.Reasoning == "" {
			return nil, nil, fmt.Errorf("invalid suggestion %d: missing reasoning", i+1)
		}
		// Set default priority if not provided
		if suggestion.Priority == "" {
			tailoringResponse.Suggestions[i].Priority = "high"
		}
	}

	cp.logger.Info("Successfully parsed and validated resume tailoring response")

	return &tailoringResponse.TailoredResume, tailoringResponse.Suggestions, nil
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
