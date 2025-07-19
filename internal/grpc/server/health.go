package server

import (
	"context"
	"time"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/pkg/utils"
)

// HealthCheck implements the HealthCheck gRPC method
func (s *Server) HealthCheck(ctx context.Context, req *letrazv1.HealthCheckRequest) (*letrazv1.HealthCheckResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Debug("gRPC health check request received", map[string]interface{}{
		"request_id": requestID,
		"method":     "HealthCheck",
	})

	// Use simple uptime of 60 seconds for testing to avoid any calculation issues
	uptimeSeconds := int64(60)

	// Create minimal response without map field to test for serialization issues
	response := &letrazv1.HealthCheckResponse{
		Status:        "healthy",
		Timestamp:     time.Now().Format(time.RFC3339),
		Version:       "1.0.0",
		UptimeSeconds: uptimeSeconds,
		// Temporarily remove Checks map to test if it's causing the size issue
	}

	s.logger.Debug("gRPC health check completed successfully", map[string]interface{}{
		"request_id":     requestID,
		"status":         response.Status,
		"uptime_seconds": response.UptimeSeconds,
	})

	return response, nil
}
