package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper/engines/headed"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

var screenshotValidator = validator.New()

// ResumeScreenshotHandler handles the POST /api/v1/resume/screenshot endpoint
func ResumeScreenshotHandler(cfg *config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		// Set request ID in context
		c.Set("request_id", requestID)

		logger.Info("Processing resume screenshot request", map[string]interface{}{
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

			return c.JSON(http.StatusBadRequest, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Invalid request body: " + err.Error(),
				Timestamp: time.Now(),
				Error:     "INVALID_REQUEST",
				RequestID: requestID,
			})
		}

		// Validate request
		if err := screenshotValidator.Struct(&req); err != nil {
			logger.Error("Request validation failed", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusBadRequest, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Validation failed: " + err.Error(),
				Timestamp: time.Now(),
				Error:     "VALIDATION_FAILED",
				RequestID: requestID,
			})
		}

		// Validate configuration
		if cfg.Resume.Client.PreviewToken == "" {
			logger.Error("Resume preview token not configured", map[string]interface{}{
				"request_id": requestID,
			})

			return c.JSON(http.StatusInternalServerError, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Resume preview service not configured",
				Timestamp: time.Now(),
				Error:     "CONFIGURATION_ERROR",
				RequestID: requestID,
			})
		}

		// Create screenshot service
		screenshotService := headed.NewScreenshotService(cfg)
		defer screenshotService.Cleanup()

		// Check if screenshot service is healthy
		if !screenshotService.IsHealthy() {
			logger.Error("Screenshot service is not healthy", map[string]interface{}{
				"request_id": requestID,
			})

			return c.JSON(http.StatusServiceUnavailable, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Screenshot service is not available",
				Timestamp: time.Now(),
				Error:     "SERVICE_UNAVAILABLE",
				RequestID: requestID,
			})
		}

		// Create DigitalOcean Spaces client
		spacesClient, err := utils.NewSpacesClient(cfg)
		if err != nil {
			logger.Error("Failed to create DigitalOcean Spaces client", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusInternalServerError, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Storage service not available",
				Timestamp: time.Now(),
				Error:     "STORAGE_ERROR",
				RequestID: requestID,
			})
		}

		// Check if Spaces client is healthy
		if !spacesClient.IsHealthy() {
			logger.Error("DigitalOcean Spaces is not healthy", map[string]interface{}{
				"request_id": requestID,
			})

			return c.JSON(http.StatusServiceUnavailable, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Storage service is not available",
				Timestamp: time.Now(),
				Error:     "STORAGE_UNAVAILABLE",
				RequestID: requestID,
			})
		}

		// Create context with timeout for screenshot operation
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		logger.Info("Capturing resume screenshot", map[string]interface{}{
			"request_id": requestID,
			"resume_id":  req.ResumeID,
		})

		// Capture the screenshot
		screenshotData, err := screenshotService.CaptureResumeScreenshot(ctx, req.ResumeID)
		if err != nil {
			logger.Error("Failed to capture screenshot", map[string]interface{}{
				"request_id": requestID,
				"resume_id":  req.ResumeID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusInternalServerError, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Failed to capture screenshot: " + err.Error(),
				Timestamp: time.Now(),
				Error:     "SCREENSHOT_FAILED",
				RequestID: requestID,
			})
		}

		logger.Info("Uploading screenshot to DigitalOcean Spaces", map[string]interface{}{
			"request_id": requestID,
			"resume_id":  req.ResumeID,
			"size_bytes": len(screenshotData),
		})

		// Upload screenshot to DigitalOcean Spaces
		screenshotURL, err := spacesClient.UploadScreenshot(req.ResumeID, screenshotData)
		if err != nil {
			logger.Error("Failed to upload screenshot", map[string]interface{}{
				"request_id": requestID,
				"resume_id":  req.ResumeID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusInternalServerError, models.ResumeScreenshotResponse{
				Status:    "FAILURE",
				Message:   "Failed to upload screenshot: " + err.Error(),
				Timestamp: time.Now(),
				Error:     "UPLOAD_FAILED",
				RequestID: requestID,
			})
		}

		logger.Info("Resume screenshot generated successfully", map[string]interface{}{
			"request_id":     requestID,
			"resume_id":      req.ResumeID,
			"screenshot_url": screenshotURL,
		})

		// Return success response
		return c.JSON(http.StatusOK, models.ResumeScreenshotResponse{
			Status:        "SUCCESS",
			Message:       "Screenshot generated successfully",
			Timestamp:     time.Now(),
			ScreenshotURL: screenshotURL,
			RequestID:     requestID,
		})
	}
}
