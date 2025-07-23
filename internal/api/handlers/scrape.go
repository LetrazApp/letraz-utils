package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var validate = validator.New()

// ScrapeHandler handles job scraping requests asynchronously with immediate process ID response
func ScrapeHandler(cfg *config.Config, poolManager *workers.PoolManager, taskManager background.TaskManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		logger.Info("Async scrape request received", map[string]interface{}{"request_id": requestID})

		// Parse request body
		var req models.ScrapeRequest
		if err := c.Bind(&req); err != nil {
			logger.Error("Failed to bind request", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"invalid_request",
				"Invalid request format: "+err.Error(),
			))
		}

		// Validate request
		if err := validate.Struct(&req); err != nil {
			logger.Error("Request validation failed", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Request validation failed: "+err.Error(),
			))
		}

		// Additional validation - either URL or description must be provided
		if req.URL == "" && req.Description == "" {
			logger.Error("Neither URL nor description provided", map[string]interface{}{
				"request_id": requestID,
			})
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Either URL or description is required",
			))
		}

		// Both URL and description cannot be provided
		if req.URL != "" && req.Description != "" {
			logger.Error("Both URL and description provided", map[string]interface{}{
				"request_id": requestID,
			})
			return c.JSON(http.StatusBadRequest, models.CreateAsyncErrorResponse(
				"validation_failed",
				"Cannot provide both URL and description - choose one",
			))
		}

		// Generate process ID for background task
		processID := utils.GenerateScrapeProcessID()

		logger.Info("Submitting scrape task for background processing", map[string]interface{}{
			"request_id":  requestID,
			"process_id":  processID,
			"url":         req.URL,
			"description": req.Description,
			"mode":        getProcessingModeFromScrapeRequest(req),
		})

		// Submit task to background task manager
		ctx := c.Request().Context()
		err := taskManager.SubmitScrapeTask(ctx, processID, req, poolManager)
		if err != nil {
			logger.Error("Failed to submit background scrape task", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusInternalServerError, models.CreateAsyncErrorResponse(
				"task_submission_failed",
				fmt.Sprintf("Failed to submit scraping task: %v", err),
				processID,
			))
		}

		// Return immediate response with process ID
		response := models.CreateAsyncScrapeResponse(processID)

		logger.Info("Scrape task submitted successfully for background processing", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"url":        req.URL,
		})

		return c.JSON(http.StatusAccepted, response)
	}
}

// getProcessingModeFromScrapeRequest returns the processing mode based on the scrape request
func getProcessingModeFromScrapeRequest(req models.ScrapeRequest) string {
	if req.Description != "" {
		return "description"
	}
	return "url"
}
