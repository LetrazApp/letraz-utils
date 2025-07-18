package handlers

import (
	"net/http"
	"time"

	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"

	"github.com/labstack/echo/v4"
)

// WorkerStatsHandler returns worker pool statistics
func WorkerStatsHandler(poolManager *workers.PoolManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		logger.Info("Worker stats request received", map[string]interface{}{"request_id": requestID})

		stats, err := poolManager.GetStats()
		if err != nil {
			logger.Error("Failed to get worker stats", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:     "stats_unavailable",
				Message:   "Worker pool statistics are not available",
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		response := map[string]interface{}{
			"success":    true,
			"stats":      stats,
			"request_id": requestID,
			"timestamp":  time.Now(),
		}

		logger.Info("Worker stats retrieved successfully", map[string]interface{}{
			"worker_count": stats.WorkerCount,
		})
		return c.JSON(http.StatusOK, response)
	}
}

// WorkerHealthHandler returns worker pool health status
func WorkerHealthHandler(poolManager *workers.PoolManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()

		healthy := poolManager.IsHealthy()
		status := "healthy"
		httpStatus := http.StatusOK

		if !healthy {
			status = "unhealthy"
			httpStatus = http.StatusServiceUnavailable
		}

		response := map[string]interface{}{
			"success":    healthy,
			"status":     status,
			"request_id": requestID,
			"timestamp":  time.Now(),
		}

		return c.JSON(httpStatus, response)
	}
}

// DomainStatsHandler returns rate limiting statistics for a specific domain
func DomainStatsHandler(poolManager *workers.PoolManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		domain := c.Param("domain")
		if domain == "" {
			return c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:     "missing_domain",
				Message:   "Domain parameter is required",
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		logger.Info("Domain stats request received", map[string]interface{}{"domain": domain})

		stats, err := poolManager.GetDomainStats(domain)
		if err != nil {
			logger.Error("Failed to get domain stats", map[string]interface{}{"error": err})
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:     "stats_unavailable",
				Message:   "Domain statistics are not available",
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		response := map[string]interface{}{
			"success":    true,
			"domain":     domain,
			"stats":      stats,
			"request_id": requestID,
			"timestamp":  time.Now(),
		}

		logger.Info("Domain stats retrieved successfully", map[string]interface{}{
			"domain": domain,
		})
		return c.JSON(http.StatusOK, response)
	}
}

// WorkerStatusResponse represents the status of the worker pool
type WorkerStatusResponse struct {
	Success        bool                   `json:"success"`
	Status         string                 `json:"status"`
	WorkerCount    int                    `json:"worker_count"`
	QueueSize      int                    `json:"queue_size"`
	JobsProcessed  int64                  `json:"jobs_processed"`
	JobsQueued     int64                  `json:"jobs_queued"`
	JobsSuccessful int64                  `json:"jobs_successful"`
	JobsFailed     int64                  `json:"jobs_failed"`
	Uptime         time.Duration          `json:"uptime"`
	Details        map[string]interface{} `json:"details,omitempty"`
	RequestID      string                 `json:"request_id"`
	Timestamp      time.Time              `json:"timestamp"`
}

// DetailedWorkerStatusHandler returns detailed worker pool status
func DetailedWorkerStatusHandler(poolManager *workers.PoolManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		logger.Info("Detailed worker status request received", map[string]interface{}{"request_id": requestID})

		stats, err := poolManager.GetStats()
		if err != nil {
			logger.Error("Failed to get detailed worker stats", map[string]interface{}{
				"request_id": requestID,
				"error":      err,
			})
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:     "stats_unavailable",
				Message:   "Detailed worker statistics are not available",
				RequestID: requestID,
				Timestamp: time.Now(),
			})
		}

		healthy := poolManager.IsHealthy()
		status := "healthy"
		if !healthy {
			status = "unhealthy"
		}

		response := WorkerStatusResponse{
			Success:        healthy,
			Status:         status,
			WorkerCount:    stats.WorkerCount,
			QueueSize:      stats.QueueCapacity,
			JobsProcessed:  stats.PoolStats.JobsProcessed,
			JobsQueued:     stats.PoolStats.JobsQueued,
			JobsSuccessful: stats.PoolStats.JobsSuccessful,
			JobsFailed:     stats.PoolStats.JobsFailed,
			Details: map[string]interface{}{
				"rate_limiter_stats":      stats.RateLimiterStats,
				"average_processing_time": stats.PoolStats.AverageProcessingTime,
				"total_processing_time":   stats.PoolStats.TotalProcessingTime,
			},
			RequestID: requestID,
			Timestamp: time.Now(),
		}

		logger.Info("Detailed worker status retrieved successfully", map[string]interface{}{
			"worker_count":    stats.WorkerCount,
			"jobs_processed":  stats.PoolStats.JobsProcessed,
			"jobs_successful": stats.PoolStats.JobsSuccessful,
			"jobs_failed":     stats.PoolStats.JobsFailed,
		})

		return c.JSON(http.StatusOK, response)
	}
}
