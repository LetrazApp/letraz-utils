package server

import (
	"context"
	"time"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/pkg/utils"
)

var startTime = time.Now()

// HealthCheck implements the HealthCheck gRPC method
func (s *Server) HealthCheck(ctx context.Context, req *letrazv1.HealthCheckRequest) (*letrazv1.HealthCheckResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Debug("gRPC health check request received", map[string]interface{}{
		"request_id": requestID,
		"method":     "HealthCheck",
	})

	// Calculate uptime in seconds
	uptime := time.Since(startTime)
	uptimeSeconds := int64(uptime.Seconds())

	// Prepare health checks - similar to HTTP health endpoint
	checks := map[string]string{
		"api": "ok",
	}

	// TODO: Add checks for external dependencies
	// - LLM API connectivity
	// - Worker pool status
	// - etc.

	// Create response following the same pattern as HTTP health endpoint
	response := &letrazv1.HealthCheckResponse{
		Status:        "healthy",
		Timestamp:     time.Now().Format(time.RFC3339),
		Version:       "1.0.0", // TODO: Get from build info
		UptimeSeconds: uptimeSeconds,
		Checks:        checks,
	}

	s.logger.Debug("gRPC health check completed successfully", map[string]interface{}{
		"request_id":     requestID,
		"status":         response.Status,
		"uptime_seconds": response.UptimeSeconds,
	})

	return response, nil
}
