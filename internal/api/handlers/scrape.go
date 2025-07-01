package handlers

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"net/http"
	"time"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/scraper/engines/headed"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ScrapeHandler handles job scraping requests
func ScrapeHandler(cfg *config.Config) echo.HandlerFunc {
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

		// Determine which engine to use
		engine := "headed" // Default to headed engine
		if req.Options != nil && req.Options.Engine != "" {
			engine = req.Options.Engine
		}

		// Create scraper based on engine type
		var job *models.JobPosting
		var err error

		ctx := c.Request().Context()

		switch engine {
		case "headed", "auto":
			scraper := headed.NewRodScraper(cfg)
			defer scraper.Cleanup()

			job, err = scraper.ScrapeJob(ctx, req.URL, req.Options)
			if err != nil {
				logger.WithError(err).Error("Rod scraper failed")
				return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
					Error:     "scraping_failed",
					Message:   fmt.Sprintf("Failed to scrape job posting: %v", err),
					RequestID: requestID,
					Timestamp: time.Now(),
				})
			}

		default:
			logger.WithField("engine", engine).Error("Unsupported scraping engine")
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:     "unsupported_engine",
				Message:   fmt.Sprintf("Unsupported scraping engine: %s", engine),
				RequestID: requestID,
				Timestamp: time.Now(),
			})
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
