package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/api/validation"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var resumeValidator = validator.New()

func init() {
	// Register shared resume validators
	validation.RegisterResumeValidators(resumeValidator)
}

// TailorResumeHandler handles the POST /api/v1/resume/tailor endpoint asynchronously
func TailorResumeHandler(cfg *config.Config, llmManager *llm.Manager, taskManager background.TaskManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		// Set request ID in context
		c.Set("request_id", requestID)

		logger.Info("Processing async resume tailoring request", map[string]interface{}{
			"request_id": requestID,
			"endpoint":   "/api/v1/resume/tailor",
			"method":     "POST",
		})

		// Parse and validate request body
		var req models.TailorResumeRequest
		if err := c.Bind(&req); err != nil {
			logger.Error("Failed to parse request body", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"invalid_request",
				"Invalid request body: "+err.Error(),
			))
		}

		// Validate request
		if err := resumeValidator.Struct(&req); err != nil {
			logger.Error("Request validation failed", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Request validation failed: "+err.Error(),
			))
		}

		// Validate that required fields are present
		if req.BaseResume.ID == "" {
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Base resume ID is required",
			))
		}

		if req.Job.Title == "" {
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Job title is required",
			))
		}

		if req.Job.CompanyName == "" {
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Job company name is required",
			))
		}

		if req.ResumeID == "" {
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Resume ID is required",
			))
		}

		// Generate process ID for background task
		processID := utils.GenerateTailorProcessID()

		logger.Info("Submitting resume tailoring task for background processing", map[string]interface{}{
			"request_id":     requestID,
			"process_id":     processID,
			"base_resume_id": req.BaseResume.ID,
			"resume_id":      req.ResumeID,
			"job_title":      req.Job.Title,
			"company":        req.Job.CompanyName,
			"sections_count": len(req.BaseResume.Sections),
		})

		// Submit task to background task manager
		ctx := c.Request().Context()
		err := taskManager.SubmitTailorTask(ctx, processID, req, llmManager, cfg)
		if err != nil {
			logger.Error("Failed to submit background tailor task", map[string]interface{}{"error": err})
			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"task_submission_failed",
				fmt.Sprintf("Failed to submit resume tailoring task: %v", err),
				processID,
			))
		}

		// Return immediate response with process ID
		response := models.CreateAsyncTailorResponse(processID)

		logger.Info("Resume tailoring task submitted successfully for background processing", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"resume_id":  req.ResumeID,
		})

		return c.JSON(http.StatusAccepted, response)
	}
}
