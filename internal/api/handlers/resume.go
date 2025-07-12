package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var resumeValidator = validator.New()

// TailorResumeHandler handles the POST /api/v1/resume/tailor endpoint asynchronously
func TailorResumeHandler(cfg *config.Config, llmManager *llm.Manager, taskManager background.TaskManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := utils.GetLogger()

		// Set request ID in context
		c.Set("request_id", requestID)

		logger.WithFields(logrus.Fields{
			"request_id": requestID,
			"endpoint":   "/api/v1/resume/tailor",
			"method":     "POST",
		}).Info("Processing async resume tailoring request")

		// Parse and validate request body
		var req models.TailorResumeRequest
		if err := c.Bind(&req); err != nil {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
				"error":      err.Error(),
			}).Error("Failed to parse request body")

			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"invalid_request",
				"Invalid request body: "+err.Error(),
			))
		}

		// Validate request
		if err := resumeValidator.Struct(&req); err != nil {
			logger.WithFields(logrus.Fields{
				"request_id": requestID,
				"error":      err.Error(),
			}).Error("Request validation failed")

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

		logger.WithFields(logrus.Fields{
			"request_id":     requestID,
			"process_id":     processID,
			"base_resume_id": req.BaseResume.ID,
			"resume_id":      req.ResumeID,
			"job_title":      req.Job.Title,
			"company":        req.Job.CompanyName,
			"sections_count": len(req.BaseResume.Sections),
		}).Info("Submitting resume tailoring task for background processing")

		// Submit task to background task manager
		ctx := c.Request().Context()
		err := taskManager.SubmitTailorTask(ctx, processID, req, llmManager, cfg)
		if err != nil {
			logger.WithError(err).Error("Failed to submit background tailor task")
			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"task_submission_failed",
				fmt.Sprintf("Failed to submit resume tailoring task: %v", err),
				processID,
			))
		}

		// Return immediate response with process ID
		response := models.CreateAsyncTailorResponse(processID)

		logger.WithFields(logrus.Fields{
			"request_id": requestID,
			"process_id": processID,
			"resume_id":  req.ResumeID,
		}).Info("Resume tailoring task submitted successfully for background processing")

		return c.JSON(http.StatusAccepted, response)
	}
}
