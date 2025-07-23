package server

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// ScrapeJob implements the ScrapeJob gRPC method (async processing)
func (s *Server) ScrapeJob(ctx context.Context, req *letrazv1.ScrapeJobRequest) (*letrazv1.ScrapeJobResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Info("gRPC async scrape request received", map[string]interface{}{
		"request_id":  requestID,
		"url":         req.GetUrl(),
		"description": req.GetDescription(),
		"method":      "ScrapeJob",
	})

	// Validate request - either URL or description must be provided
	if req.GetUrl() == "" && req.GetDescription() == "" {
		return nil, status.Error(codes.InvalidArgument, "either URL or description is required")
	}

	// Both URL and description cannot be provided
	if req.GetUrl() != "" && req.GetDescription() != "" {
		return nil, status.Error(codes.InvalidArgument, "cannot provide both URL and description - choose one")
	}

	// Convert gRPC request to internal model
	scrapeReq := models.ScrapeRequest{
		URL:         req.GetUrl(),
		Description: req.GetDescription(),
		Options:     convertGRPCOptionsToModel(req.GetOptions()),
	}

	// Generate process ID for background task
	processID := utils.GenerateScrapeProcessID()

	s.logger.Info("Submitting scrape task for background processing", map[string]interface{}{
		"request_id":  requestID,
		"process_id":  processID,
		"url":         req.GetUrl(),
		"description": req.GetDescription(),
		"mode":        getProcessingMode(req.GetUrl(), req.GetDescription()),
	})

	// Submit task to background task manager (async processing)
	err := s.taskManager.SubmitScrapeTask(ctx, processID, scrapeReq, s.poolManager)
	if err != nil {
		s.logger.Error("Failed to submit background scrape task", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"error":      err.Error(),
		})

		return &letrazv1.ScrapeJobResponse{
			ProcessId: processID,
			Status:    "FAILURE",
			Message:   "Failed to submit scraping task for background processing",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "TASK_SUBMISSION_FAILED: " + err.Error(),
		}, nil
	}

	// Return immediate response with process ID (async pattern)
	s.logger.Info("Scrape task submitted successfully for background processing", map[string]interface{}{
		"request_id":  requestID,
		"process_id":  processID,
		"url":         req.GetUrl(),
		"description": req.GetDescription(),
		"mode":        getProcessingMode(req.GetUrl(), req.GetDescription()),
	})

	return &letrazv1.ScrapeJobResponse{
		ProcessId: processID,
		Status:    "ACCEPTED",
		Message:   "Scraping request accepted for background processing",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Error:     "",
	}, nil
}

// getProcessingMode returns a string indicating the processing mode
func getProcessingMode(url, description string) string {
	if description != "" {
		return "description"
	}
	return "url"
}

// convertGRPCOptionsToModel converts gRPC ScrapeOptions to internal model
func convertGRPCOptionsToModel(options *letrazv1.ScrapeOptions) *models.ScrapeOptions {
	if options == nil {
		return nil
	}

	return &models.ScrapeOptions{
		Engine:      options.GetEngine(),
		Timeout:     time.Duration(options.GetTimeoutSeconds()) * time.Second,
		LLMProvider: options.GetLlmProvider(),
		UserAgent:   options.GetUserAgent(),
		Proxy:       options.GetProxy(),
	}
}

// Helper functions removed since we only return async process info, not job data
