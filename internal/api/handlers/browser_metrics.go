package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper/engines/headed"
	"letraz-utils/pkg/utils"
)

// BrowserMetricsResponse represents the browser pool metrics response
type BrowserMetricsResponse struct {
	Status  string                 `json:"status"`
	Metrics map[string]interface{} `json:"metrics"`
}

// BrowserMetricsHandler returns current browser pool metrics
func BrowserMetricsHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		requestID := utils.GenerateRequestID()
		logger := logging.GetGlobalLogger()

		logger.Info("Browser metrics request received", map[string]interface{}{
			"request_id": requestID,
			"endpoint":   "/api/v1/metrics/browser",
		})

		// Get global browser pool
		globalPool, err := headed.GetGlobalBrowserPool()
		if err != nil {
			logger.Error("Failed to get global browser pool for metrics", map[string]interface{}{
				"request_id": requestID,
				"error":      err.Error(),
			})

			return c.JSON(http.StatusServiceUnavailable, BrowserMetricsResponse{
				Status: "error",
				Metrics: map[string]interface{}{
					"error": "Browser pool not available",
				},
			})
		}

		// Get metrics
		metrics := globalPool.GetMetrics()

		response := BrowserMetricsResponse{
			Status: "ok",
			Metrics: map[string]interface{}{
				"total_browsers_created":   metrics.TotalBrowsersCreated,
				"total_browsers_closed":    metrics.TotalBrowsersClosed,
				"current_active_browsers":  metrics.CurrentActiveBrowsers,
				"available_browsers":       metrics.AvailableBrowsers,
				"queued_requests":          metrics.QueuedRequests,
				"average_acquisition_time": metrics.AverageAcquisitionTime.String(),
				"is_healthy":               globalPool.IsHealthy(),
			},
		}

		logger.Info("Browser metrics response sent", map[string]interface{}{
			"request_id":              requestID,
			"current_active_browsers": metrics.CurrentActiveBrowsers,
			"available_browsers":      metrics.AvailableBrowsers,
		})

		return c.JSON(http.StatusOK, response)
	}
}
