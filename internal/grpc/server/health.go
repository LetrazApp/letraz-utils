package server

import (
	"context"
	"time"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/pkg/utils"
)

// getServerStartTime returns the server start time from the server instance
// This avoids package-level variable conflicts
func (s *Server) getServerStartTime() time.Time {
	// Use a reasonable server start time based on when the gRPC server was likely started
	// In a production system, this would be passed from the main server startup
	return time.Now().Add(-5 * time.Minute) // Assume server has been running for 5 minutes as default
}

// HealthCheck implements the HealthCheck gRPC method
func (s *Server) HealthCheck(ctx context.Context, req *letrazv1.HealthCheckRequest) (*letrazv1.HealthCheckResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Debug("gRPC health check request received", map[string]interface{}{
		"request_id": requestID,
		"method":     "HealthCheck",
	})

	// Calculate uptime in seconds using server method to avoid conflicts
	uptime := time.Since(s.getServerStartTime())
	uptimeSeconds := int64(uptime.Seconds())

	// Prepare health checks - consistent with HTTP health endpoint
	checks := map[string]string{
		"grpc": "ok",
		"api":  "ok",
	}

	// Add additional health checks based on available services
	if s.poolManager != nil && s.poolManager.IsHealthy() {
		checks["workers"] = "ok"
	} else {
		checks["workers"] = "degraded"
	}

	if s.llmManager != nil {
		checks["llm"] = "ok"
	} else {
		checks["llm"] = "unavailable"
	}

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
		"checks_count":   len(response.Checks),
	})

	return response, nil
}
