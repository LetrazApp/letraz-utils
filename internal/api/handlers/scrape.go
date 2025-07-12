package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var validate = validator.New()

// ScrapeHandler handles job scraping requests asynchronously with immediate process ID response
func ScrapeHandler(cfg *config.Config, poolManager *workers.PoolManager, taskManager background.TaskManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := utils.LogWithRequestID(requestID)

		logger.Info("Async scrape request received")

		// Parse request body
		var req models.ScrapeRequest
		if err := c.Bind(&req); err != nil {
			logger.WithError(err).Error("Failed to bind request")
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"invalid_request",
				"Invalid request format: "+err.Error(),
			))
		}

		// Validate request
		if err := validate.Struct(&req); err != nil {
			logger.WithError(err).Error("Request validation failed")
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Request validation failed: "+err.Error(),
			))
		}

		// Generate process ID for background task
		processID := utils.GenerateScrapeProcessID()

		logger.WithFields(map[string]interface{}{
			"process_id": processID,
			"url":        req.URL,
		}).Info("Submitting scrape task for background processing")

		// Submit task to background task manager
		ctx := c.Request().Context()
		err := taskManager.SubmitScrapeTask(ctx, processID, req, poolManager)
		if err != nil {
			logger.WithError(err).Error("Failed to submit background scrape task")
			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"task_submission_failed",
				fmt.Sprintf("Failed to submit scraping task: %v", err),
				processID,
			))
		}

		// Return immediate response with process ID
		response := models.CreateAsyncScrapeResponse(processID)

		logger.WithFields(map[string]interface{}{
			"process_id": processID,
			"url":        req.URL,
		}).Info("Scrape task submitted successfully for background processing")

		return c.JSON(http.StatusAccepted, response)
	}
}
