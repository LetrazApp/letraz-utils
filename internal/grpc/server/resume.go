package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/internal/exporter"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
	"strings"
)

// TailorResume implements the TailorResume gRPC method (async processing)
func (s *Server) TailorResume(ctx context.Context, req *letrazv1.TailorResumeRequest) (*letrazv1.TailorResumeResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Info("gRPC async tailor resume request received", map[string]interface{}{
		"request_id": requestID,
		"resume_id":  req.GetResumeId(),
		"method":     "TailorResume",
	})

	// Validate request
	if req.GetBaseResume() == nil {
		return nil, status.Error(codes.InvalidArgument, "Base resume is required")
	}
	if req.GetJob() == nil {
		return nil, status.Error(codes.InvalidArgument, "Job information is required")
	}
	if req.GetResumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Resume ID is required")
	}

	// Check LLM manager health
	if !s.llmManager.IsHealthy() {
		s.logger.Error("LLM manager is not healthy", map[string]interface{}{
			"request_id": requestID,
		})

		return &letrazv1.TailorResumeResponse{
			ProcessId: "",
			Status:    "FAILURE",
			Message:   "LLM manager is not healthy",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "LLM_UNAVAILABLE: LLM manager is not healthy",
		}, nil
	}

	// Convert gRPC request to internal models
	baseResume := convertGRPCBaseResumeToModel(req.GetBaseResume())
	job := convertGRPCJobToModel(req.GetJob())

	// Create internal tailor request
	tailorReq := models.TailorResumeRequest{
		BaseResume: *baseResume,
		Job:        *job,
		ResumeID:   req.GetResumeId(),
	}

	// Generate process ID for background task
	processID := utils.GenerateTailorProcessID()

	s.logger.Info("Submitting resume tailoring task for background processing", map[string]interface{}{
		"request_id":     requestID,
		"process_id":     processID,
		"base_resume_id": req.GetBaseResume().GetId(),
		"resume_id":      req.GetResumeId(),
		"job_title":      req.GetJob().GetTitle(),
		"company":        req.GetJob().GetCompanyName(),
	})

	// Submit task to background task manager (async processing)
	err := s.taskManager.SubmitTailorTask(ctx, processID, tailorReq, s.llmManager, s.cfg)
	if err != nil {
		s.logger.Error("Failed to submit background tailor task", map[string]interface{}{
			"request_id": requestID,
			"process_id": processID,
			"error":      err.Error(),
		})

		return &letrazv1.TailorResumeResponse{
			ProcessId: processID,
			Status:    "FAILURE",
			Message:   "Failed to submit resume tailoring task for background processing",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "TASK_SUBMISSION_FAILED: " + err.Error(),
		}, nil
	}

	// Return immediate response with process ID (async pattern)
	s.logger.Info("Resume tailoring task submitted successfully for background processing", map[string]interface{}{
		"request_id": requestID,
		"process_id": processID,
		"resume_id":  req.GetResumeId(),
	})

	return &letrazv1.TailorResumeResponse{
		ProcessId: processID,
		Status:    "ACCEPTED",
		Message:   "Resume tailoring request accepted for background processing",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Error:     "",
	}, nil
}

// convertGRPCBaseResumeToModel converts gRPC BaseResume to internal model
func convertGRPCBaseResumeToModel(grpcResume *letrazv1.BaseResume) *models.BaseResume {
	if grpcResume == nil {
		return nil
	}

	// Convert User (guard nil)
	var user models.User
	if grpcUser := grpcResume.GetUser(); grpcUser != nil {
		user = models.User{
			ID:          grpcUser.GetId(),
			Title:       getStringPointer(grpcUser.GetTitle()),
			FirstName:   grpcUser.GetFirstName(),
			LastName:    grpcUser.GetLastName(),
			Email:       grpcUser.GetEmail(),
			Phone:       grpcUser.GetPhone(),
			DOB:         getStringPointer(grpcUser.GetDob()),
			Nationality: getStringPointer(grpcUser.GetNationality()),
			Address:     grpcUser.GetAddress(),
			City:        grpcUser.GetCity(),
			Postal:      grpcUser.GetPostal(),
			Country:     getStringPointer(grpcUser.GetCountry()),
			Website:     grpcUser.GetWebsite(),
			ProfileText: grpcUser.GetProfileText(),
		}

		// Parse timestamps if provided
		if grpcUser.GetCreatedAt() != "" {
			if createdAt, err := time.Parse(time.RFC3339Nano, grpcUser.GetCreatedAt()); err == nil {
				user.CreatedAt = createdAt
			}
		}
		if grpcUser.GetUpdatedAt() != "" {
			if updatedAt, err := time.Parse(time.RFC3339Nano, grpcUser.GetUpdatedAt()); err == nil {
				user.UpdatedAt = updatedAt
			}
		}
	}

	// Convert Sections
	sections := make([]models.ResumeSection, len(grpcResume.GetSections()))
	for i, section := range grpcResume.GetSections() {
		// Convert google.protobuf.Struct to interface{}
		var sectionData interface{}
		if section.GetData() != nil {
			sectionData = section.GetData().AsMap()
		}

		sections[i] = models.ResumeSection{
			ID:     section.GetId(),
			Resume: section.GetResume(),
			Index:  int(section.GetIndex()),
			Type:   section.GetType(),
			Data:   sectionData,
		}
	}

	return &models.BaseResume{
		ID:       grpcResume.GetId(),
		Base:     grpcResume.GetBase(),
		User:     user,
		Sections: sections,
	}
}

// getStringPointer returns a pointer to a string if it's not empty, nil otherwise
func getStringPointer(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// convertGRPCJobToModel converts gRPC Job to internal Job model
func convertGRPCJobToModel(grpcJob *letrazv1.Job) *models.Job {
	if grpcJob == nil {
		return nil
	}

	// Convert salary object
	salary := models.Salary{
		Currency: grpcJob.GetSalary().GetCurrency(),
		Min:      int(grpcJob.GetSalary().GetMin()),
		Max:      int(grpcJob.GetSalary().GetMax()),
	}

	return &models.Job{
		Title:            grpcJob.GetTitle(),
		JobURL:           grpcJob.GetJobUrl(),
		CompanyName:      grpcJob.GetCompanyName(),
		Location:         grpcJob.GetLocation(),
		Currency:         grpcJob.GetSalary().GetCurrency(),
		Salary:           salary,
		Requirements:     grpcJob.GetRequirements(),
		Description:      grpcJob.GetDescription(),
		Responsibilities: grpcJob.GetResponsibilities(),
		Benefits:         grpcJob.GetBenefits(),
	}
}

// GenerateScreenshot implements the GenerateScreenshot gRPC method (async)
func (s *Server) GenerateScreenshot(ctx context.Context, req *letrazv1.ResumeScreenshotRequest) (*letrazv1.ResumeScreenshotResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Info("gRPC async resume screenshot request received", map[string]interface{}{
		"request_id": requestID,
		"resume_id":  req.GetResumeId(),
		"method":     "GenerateScreenshot",
	})

	// Validate request
	if req.GetResumeId() == "" {
		return &letrazv1.ResumeScreenshotResponse{
			Status:    "FAILURE",
			Message:   "Resume ID is required",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "INVALID_ARGUMENT",
		}, nil
	}

	// Validate configuration
	if s.cfg.Resume.Client.PreviewToken == "" {
		s.logger.Error("Resume preview token not configured", map[string]interface{}{
			"request_id": requestID,
		})

		return &letrazv1.ResumeScreenshotResponse{
			Status:    "FAILURE",
			Message:   "Resume preview service not configured",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "CONFIGURATION_ERROR",
		}, nil
	}

	// Convert gRPC request to internal model
	screenshotReq := models.ResumeScreenshotRequest{
		ResumeID: req.GetResumeId(),
	}

	// Generate process ID for background task
	processID := utils.GenerateScreenshotProcessID()

	s.logger.Info("Submitting gRPC screenshot task for background processing", map[string]interface{}{
		"request_id": requestID,
		"process_id": processID,
		"resume_id":  req.GetResumeId(),
	})

	// Submit task to background task manager
	err := s.taskManager.SubmitScreenshotTask(ctx, processID, screenshotReq, s.cfg)
	if err != nil {
		s.logger.Error("Failed to submit gRPC background screenshot task", map[string]interface{}{
			"request_id": requestID,
			"error":      err.Error(),
		})

		return &letrazv1.ResumeScreenshotResponse{
			Status:    "FAILURE",
			Message:   "Failed to submit screenshot task: " + err.Error(),
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "TASK_SUBMISSION_FAILED",
		}, nil
	}

	s.logger.Info("gRPC screenshot task submitted successfully", map[string]interface{}{
		"request_id": requestID,
		"process_id": processID,
		"resume_id":  req.GetResumeId(),
	})

	// Return success response with process ID in dedicated field
	return &letrazv1.ResumeScreenshotResponse{
		Status:    "ACCEPTED",
		Message:   "Screenshot request accepted for background processing",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		ProcessId: processID, // Process ID for tracking async task
	}, nil
}

// All conversion functions updated to match new proto structure

// ExportResume implements synchronous export of LaTeX and upload
func (s *Server) ExportResume(ctx context.Context, req *letrazv1.ExportResumeRequest) (*letrazv1.ExportResumeResponse, error) {
	requestID := utils.GenerateRequestID()

	s.logger.Info("gRPC export resume request received", map[string]interface{}{
		"request_id": requestID,
		"method":     "ExportResume",
	})

	if req.GetResume() == nil {
		return &letrazv1.ExportResumeResponse{
			Status:    "FAILURE",
			Message:   "Resume is required",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "INVALID_ARGUMENT",
		}, nil
	}

	if req.GetResume().GetId() == "" {
		return &letrazv1.ExportResumeResponse{
			Status:    "FAILURE",
			Message:   "Resume ID is required",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "INVALID_ARGUMENT",
		}, nil
	}

	if strings.TrimSpace(req.GetTheme()) == "" {
		return &letrazv1.ExportResumeResponse{
			Status:    "FAILURE",
			Message:   "Theme is required",
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     "INVALID_ARGUMENT",
		}, nil
	}

	// Convert to internal model
	resume := convertGRPCBaseResumeToModel(req.GetResume())

	// Render and upload via shared exporter
	latexURL, pdfURL, err := exporter.ExportResume(ctx, s.cfg, *resume, req.GetTheme())
	if err != nil {
		statusCode := "INTERNAL"
		message := err.Error()
		if errors.Is(err, exporter.ErrRender) {
			statusCode = "RENDER_ERROR"
			message = "Failed to render LaTeX"
		} else if errors.Is(err, exporter.ErrStorageConfig) {
			statusCode = "STORAGE_CONFIGURATION"
			message = "Storage not configured"
		} else if errors.Is(err, exporter.ErrUpload) {
			statusCode = "UPLOAD_FAILED"
			message = "Failed to upload export"
		}
		return &letrazv1.ExportResumeResponse{
			Status:    "FAILURE",
			Message:   message,
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Error:     statusCode,
		}, nil
	}

	return &letrazv1.ExportResumeResponse{
		Status:    "SUCCESS",
		Message:   "Exported successfully",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		LatexUrl:  latexURL,
		PdfUrl:    pdfURL,
	}, nil
}
