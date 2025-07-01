package handlers

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"net/http"
	"time"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/scraper/workers"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ScrapeHandler handles job scraping requests using the worker pool
func ScrapeHandler(cfg *config.Config, poolManager *workers.PoolManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		startTime := time.Now()
		requestID := utils.GenerateRequestID()
		logger := utils.LogWithRequestID(requestID)

		logger.Info("Scrape request received")

		// Parse request body
		var req models.ScrapeRequest
		if err := c.Bind(&req); err != nil {
			logger.WithError(err).Error("Failed to bind request")
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:     "invalid_request",
				Message:   "Invalid request format",
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		// Validate request
		if err := validate.Struct(&req); err != nil {
			logger.WithError(err).Error("Request validation failed")
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:     "validation_failed",
				Message:   err.Error(),
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		logger.WithField("url", req.URL).Info("Processing scrape request")

		// Submit job to worker pool
		ctx := c.Request().Context()
		result, err := poolManager.SubmitJob(ctx, req.URL, req.Options)
		if err != nil {
			logger.WithError(err).Error("Failed to submit job to worker pool")
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:     "job_submission_failed",
				Message:   fmt.Sprintf("Failed to submit scraping job: %v", err),
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		// Check if the job was successful
		if result.Error != nil {
			logger.WithError(result.Error).Error("Scraping job failed")
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:     "scraping_failed",
				Message:   fmt.Sprintf("Failed to scrape job posting: %v", result.Error),
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		job := result.Job

		// Determine engine used (from options or default)
		engine := "headed"
		if req.Options != nil && req.Options.Engine != "" {
			engine = req.Options.Engine
		}

		// Prepare response
		response := models.ScrapeResponse{
			Success:        true,
			Job:            job,
			ProcessingTime: time.Since(startTime),
			Engine:         engine,
			RequestID:      requestID,
		}

		logger.WithFields(map[string]interface{}{
			"processing_time": time.Since(startTime),
			"job_title":       job.Title,
			"company":         job.Company,
			"engine":          engine,
		}).Info("Scrape request completed successfully")

		return c.JSON(http.StatusOK, response)
	}
}
