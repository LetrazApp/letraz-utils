package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/api/validation"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/latex"
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

// ExportResumeHandler handles POST /api/v1/resume/export to render LaTeX and upload to Spaces
func ExportResumeHandler(cfg *config.Config) echo.HandlerFunc {
	type ExportRequest struct {
		Resume models.BaseResume `json:"resume" validate:"required"`
		Theme  string            `json:"theme" validate:"required"`
	}

	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()
		c.Set("request_id", requestID)

		logger.Info("Processing resume export request", map[string]interface{}{
			"request_id": requestID,
			"endpoint":   "/api/v1/resume/export",
			"method":     "POST",
		})

		var req ExportRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"status":  "FAILURE",
				"message": "Invalid request body: " + err.Error(),
				"error":   "INVALID_REQUEST",
			})
		}

		if req.Resume.ID == "" {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"status":  "FAILURE",
				"message": "Resume ID is required",
				"error":   "VALIDATION_FAILED",
			})
		}

		// Render latex
		engine := latex.NewEngine()
		latexStr, err := engine.Render(req.Resume, req.Theme)
		if err != nil {
			logger.Error("Failed to render LaTeX", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"status":  "FAILURE",
				"message": "Failed to render LaTeX",
				"error":   "RENDER_ERROR",
			})
		}

		// Upload to Spaces
		spaces, err := utils.NewSpacesClient(cfg)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"status":  "FAILURE",
				"message": "Storage not configured: " + err.Error(),
				"error":   "STORAGE_CONFIGURATION",
			})
		}
		url, err := spaces.UploadLatexExport(req.Resume.ID, "", []byte(latexStr))
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"status":  "FAILURE",
				"message": "Failed to upload export: " + err.Error(),
				"error":   "UPLOAD_FAILED",
			})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":     "SUCCESS",
			"message":    "Exported successfully",
			"export_url": url,
		})
	}
}
