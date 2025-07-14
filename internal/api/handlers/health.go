package handlers

import (
	"net/http"
	"time"

	"letraz-utils/internal/logging"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"

	"github.com/labstack/echo/v4"
)

var startTime = time.Now()

// HealthHandler handles health check requests
func HealthHandler(c echo.Context) error {
	requestID := utils.GenerateRequestID()
	logger := logging.GetGlobalLogger()

	logger.Debug("Health check requested", map[string]interface{}{"request_id": requestID})

	response := models.HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   "1.0.0", // TODO: Get from build info
		Uptime:    time.Since(startTime),
		Checks: map[string]string{
			"api": "ok",
		},
	}

	return c.JSON(http.StatusOK, response)
}

// ReadinessHandler handles readiness probe requests
func ReadinessHandler(c echo.Context) error {
	requestID := utils.GenerateRequestID()
	logger := logging.GetGlobalLogger()

	logger.Debug("Readiness check requested", map[string]interface{}{"request_id": requestID})

	// TODO: Add checks for external dependencies
	// - LLM API connectivity
	// - Worker pool status
	// - etc.

	response := models.HealthResponse{
		Status:    "ready",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Uptime:    time.Since(startTime),
		Checks: map[string]string{
			"api":     "ok",
			"workers": "ok",
			"llm":     "ok",
		},
	}

	return c.JSON(http.StatusOK, response)
}

// LivenessHandler handles liveness probe requests
func LivenessHandler(c echo.Context) error {
	requestID := utils.GenerateRequestID()
	logger := logging.GetGlobalLogger()

	logger.Debug("Liveness check requested", map[string]interface{}{"request_id": requestID})

	response := models.HealthResponse{
		Status:    "alive",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Uptime:    time.Since(startTime),
	}

	return c.JSON(http.StatusOK, response)
}

// StatusHandler provides detailed service status
func StatusHandler(c echo.Context) error {
	requestID := utils.GenerateRequestID()
	logger := logging.GetGlobalLogger()

	logger.Debug("Status check requested", map[string]interface{}{"request_id": requestID})

	// TODO: Add more detailed status information
	// - Memory usage
	// - Active requests
	// - Worker pool status
	// - Rate limiting status

	response := models.HealthResponse{
		Status:    "operational",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Uptime:    time.Since(startTime),
		Checks: map[string]string{
			"api":           "operational",
			"workers":       "operational",
			"llm":           "operational",
			"memory_usage":  "normal",
			"request_queue": "normal",
		},
	}

	return c.JSON(http.StatusOK, response)
}
