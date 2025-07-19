package handlers

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var screenshotValidator = validator.New()

// Resume ID validation pattern: rsm_ followed by alphanumeric chars, hyphens, and underscores
var resumeIDPattern = regexp.MustCompile(`^rsm_[a-zA-Z0-9_-]{10,50}$`)

func init() {
	// Register custom resume_id validator
	screenshotValidator.RegisterValidation("resume_id", validateResumeID)
}

// validateResumeID validates that the resume ID follows the expected format
func validateResumeID(fl validator.FieldLevel) bool {
	resumeID := fl.Field().String()
	return resumeIDPattern.MatchString(resumeID)
}

// ResumeScreenshotHandler handles the POST /api/v1/resume/screenshot endpoint (async)
func ResumeScreenshotHandler(cfg *config.Config, taskManager background.TaskManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		// Set request ID in context
		c.Set("request_id", requestID)

		logger.Info("Processing async resume screenshot request", map[string]interface{}{
			"request_id": requestID,
			"endpoint":   "/api/v1/resume/screenshot",
			"method":     "POST",
		})

		// Parse and validate request body
		var req models.ResumeScreenshotRequest
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
		if err := screenshotValidator.Struct(&req); err != nil {
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
		if req.ResumeID == "" {
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Resume ID is required",
			))
		}

		// Validate configuration
		if cfg.Resume.Client.PreviewToken == "" {
			logger.Error("Resume preview token not configured", map[string]interface{}{
				"request_id": requestID,
			})

			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"configuration_error",
				"Resume preview service not configured",
			))
		}

		// Generate process ID for background task
		processID := utils.GenerateScreenshotProcessID()

		logger.Info("Submitting screenshot task for background processing", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"resume_id":  req.ResumeID,
		})

		// Submit task to background task manager
		ctx := c.Request().Context()
		err := taskManager.SubmitScreenshotTask(ctx, processID, req, cfg)
		if err != nil {
			logger.Error("Failed to submit background screenshot task", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"task_submission_failed",
				fmt.Sprintf("Failed to submit screenshot task: %v", err),
				processID,
			))
		}

		// Return immediate response with process ID
		response := models.CreateAsyncScreenshotResponse(processID)

		logger.Info("Screenshot task submitted successfully for background processing", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"resume_id":  req.ResumeID,
		})

		return c.JSON(http.StatusAccepted, response)
	}
}
