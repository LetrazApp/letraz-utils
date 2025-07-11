package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var resumeValidator = validator.New()

// TailorResumeHandler handles the POST /api/v1/resume/tailor endpoint
func TailorResumeHandler(cfg *config.Config, llmManager *llm.Manager) echo.HandlerFunc {
	return func(c echo.Context) error {
		startTime := time.Now()
		requestID := utils.GenerateRequestID()
		logger := utils.GetLogger()

		// Set request ID in context
		c.Set("request_id", requestID)

		logger.WithFields(logrus.Fields{
			"request_id": requestID,
			"endpoint":   "/api/v1/resume/tailor",
			"method":     "POST",
		}).Info("Processing resume tailoring request")

		// Parse and validate request body
		var req models.TailorResumeRequest
		if err := c.Bind(&req); err != nil {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
				"error":      err.Error(),
			}).Error("Failed to parse request body")

			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Invalid request body: " + err.Error(),
			})
		}

		// Validate request
		if err := resumeValidator.Struct(&req); err != nil {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
				"error":      err.Error(),
			}).Error("Request validation failed")

			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Validation failed: " + err.Error(),
			})
		}

		// Validate that required fields are present
		if req.BaseResume.ID == "" {
			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Base resume ID is required",
			})
		}

		if req.Job.Title == "" {
			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Job title is required",
			})
		}

		if req.Job.CompanyName == "" {
			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Job company name is required",
			})
		}

		if req.ResumeID == "" {
			return c.JSON(http.StatusBadRequest, models.TailorResumeResponse{
				Success: false,
				Error:   "Resume ID is required",
			})
		}

		logger.WithFields(logrus.Fields{
			"request_id":     requestID,
			"base_resume_id": req.BaseResume.ID,
			"resume_id":      req.ResumeID,
			"job_title":      req.Job.Title,
			"company":        req.Job.CompanyName,
			"sections_count": len(req.BaseResume.Sections),
		}).Info("Resume tailoring request validated successfully")

		// Initialize Redis client for conversation history (optional)
		var redisClient *utils.RedisClient
		var redisAvailable bool

		ctx := c.Request().Context()

		// Try to initialize Redis, but don't fail if it's not available
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithFields(logrus.Fields{
						"request_id": requestID,
						"error":      fmt.Sprintf("%v", r),
					}).Warn("Redis initialization failed - continuing without conversation history")
				}
			}()

			redisClient = utils.NewRedisClient(cfg)
			if err := redisClient.Ping(ctx); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"error":      err.Error(),
				}).Warn("Redis connection failed - conversation history will not be saved")
				redisClient.Close()
				redisClient = nil
			} else {
				redisAvailable = true
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
				}).Debug("Redis connection successful")
			}
		}()

		// Only use Redis if it's available
		if redisAvailable && redisClient != nil {
			defer redisClient.Close()

			// Create conversation thread with resumeID as threadID
			if err := redisClient.CreateConversationThread(ctx, req.ResumeID); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"resume_id":  req.ResumeID,
					"error":      err.Error(),
				}).Warn("Failed to create conversation thread - continuing without history")
			}

			// Store the system prompt as the first entry in conversation history
			systemPrompt := buildSystemPrompt(&req.BaseResume, &req.Job)
			systemEntry := utils.ConversationEntry{
				Role:    "system",
				Content: systemPrompt,
				Metadata: map[string]interface{}{
					"action":       "system_prompt",
					"prompt_type":  "resume_tailoring",
					"job_title":    req.Job.Title,
					"company_name": req.Job.CompanyName,
				},
			}

			if err := redisClient.AddConversationEntry(ctx, req.ResumeID, systemEntry); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"resume_id":  req.ResumeID,
					"error":      err.Error(),
				}).Warn("Failed to store system prompt in conversation history")
			}

			// Store the complete user request in conversation history
			userRequestData := map[string]interface{}{
				"baseResume": req.BaseResume,
				"job":        req.Job,
				"resumeId":   req.ResumeID,
				"action":     "tailor_resume_request",
			}

			userRequestJSON, _ := json.Marshal(userRequestData)
			userRequestEntry := utils.ConversationEntry{
				Role:    "user",
				Content: string(userRequestJSON),
				Metadata: map[string]interface{}{
					"action":         "tailor_resume_request",
					"resume_id":      req.BaseResume.ID,
					"job_title":      req.Job.Title,
					"company_name":   req.Job.CompanyName,
					"sections_count": len(req.BaseResume.Sections),
					"content_type":   "full_request",
				},
			}

			if err := redisClient.AddConversationEntry(ctx, req.ResumeID, userRequestEntry); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"resume_id":  req.ResumeID,
					"error":      err.Error(),
				}).Warn("Failed to store user request in conversation history")
			}
		}

		// Check LLM manager health
		if !llmManager.IsHealthy() {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
			}).Error("LLM manager is not healthy")

			return c.JSON(http.StatusServiceUnavailable, models.TailorResumeResponse{
				Success: false,
				Error:   "AI service is currently unavailable. Please try again later.",
			})
		}

		// Call LLM to tailor the resume
		tailoredResume, suggestions, rawResponse, err := llmManager.TailorResumeWithRawResponse(ctx, &req.BaseResume, &req.Job)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
				"error":      err.Error(),
			}).Error("Failed to tailor resume using LLM")

			return c.JSON(http.StatusInternalServerError, models.TailorResumeResponse{
				Success: false,
				Error:   "Failed to process resume tailoring: " + err.Error(),
			})
		}

		processingTime := time.Since(startTime)

		// Store the AI response in conversation history (if Redis is available)
		if redisAvailable && redisClient != nil {
			// Store the raw AI response
			rawResponseEntry := utils.ConversationEntry{
				Role:    "assistant",
				Content: rawResponse,
				Metadata: map[string]interface{}{
					"action":          "raw_ai_response",
					"provider":        "claude",
					"processing_time": processingTime.String(),
					"content_type":    "raw_response",
					"response_length": len(rawResponse),
				},
			}

			if err := redisClient.AddConversationEntry(ctx, req.ResumeID, rawResponseEntry); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"resume_id":  req.ResumeID,
					"error":      err.Error(),
				}).Warn("Failed to store raw AI response in conversation history")
			}

			// Store the structured AI response
			aiResponseContent := map[string]interface{}{
				"tailored_resume": tailoredResume,
				"suggestions":     suggestions,
				"action":          "tailor_resume_response",
				"processing_time": processingTime.String(),
			}

			aiResponseJSON, _ := json.Marshal(aiResponseContent)
			aiResponseEntry := utils.ConversationEntry{
				Role:    "assistant",
				Content: string(aiResponseJSON),
				Metadata: map[string]interface{}{
					"action":            "tailor_resume_response",
					"suggestions_count": len(suggestions),
					"sections_modified": len(tailoredResume.Sections),
					"processing_time":   processingTime.String(),
					"content_type":      "structured_response",
				},
			}

			if err := redisClient.AddConversationEntry(ctx, req.ResumeID, aiResponseEntry); err != nil {
				logger.WithFields(logrus.Fields{
					"request_id": requestID,
					"resume_id":  req.ResumeID,
					"error":      err.Error(),
				}).Warn("Failed to store structured AI response in conversation history")
			}
		}

		logger.WithFields(logrus.Fields{
			"request_id":        requestID,
			"resume_id":         req.ResumeID,
			"processing_time":   processingTime,
			"suggestions_count": len(suggestions),
			"sections_count":    len(tailoredResume.Sections),
		}).Info("Resume tailoring completed successfully")

		// Return successful response
		response := models.TailorResumeResponse{
			Success:     true,
			Resume:      *tailoredResume,
			Suggestions: suggestions,
			ThreadID:    req.ResumeID, // Use resumeID as threadID as per requirements
		}

		return c.JSON(http.StatusOK, response)
	}
}

// buildSystemPrompt creates the system prompt for resume tailoring
func buildSystemPrompt(baseResume *models.BaseResume, job *models.Job) string {
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
      "impact": "Emphasizing Python and Django skills would directly align with the job requirements and increase selection chances by 40%%",
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
      "suggested": "Include specific metrics from existing projects (e.g., 'improved system performance by X%%', 'handled Y requests per day')",
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
