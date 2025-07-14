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

	// Convert User
	user := models.User{
		ID:          grpcResume.GetUser().GetId(),
		Title:       getStringPointer(grpcResume.GetUser().GetTitle()),
		FirstName:   grpcResume.GetUser().GetFirstName(),
		LastName:    grpcResume.GetUser().GetLastName(),
		Email:       grpcResume.GetUser().GetEmail(),
		Phone:       grpcResume.GetUser().GetPhone(),
		DOB:         getStringPointer(grpcResume.GetUser().GetDob()),
		Nationality: getStringPointer(grpcResume.GetUser().GetNationality()),
		Address:     grpcResume.GetUser().GetAddress(),
		City:        grpcResume.GetUser().GetCity(),
		Postal:      grpcResume.GetUser().GetPostal(),
		Country:     getStringPointer(grpcResume.GetUser().GetCountry()),
		Website:     grpcResume.GetUser().GetWebsite(),
		ProfileText: grpcResume.GetUser().GetProfileText(),
	}

	// Parse timestamps if provided
	if grpcResume.GetUser().GetCreatedAt() != "" {
		if createdAt, err := time.Parse(time.RFC3339Nano, grpcResume.GetUser().GetCreatedAt()); err == nil {
			user.CreatedAt = createdAt
		}
	}
	if grpcResume.GetUser().GetUpdatedAt() != "" {
		if updatedAt, err := time.Parse(time.RFC3339Nano, grpcResume.GetUser().GetUpdatedAt()); err == nil {
			user.UpdatedAt = updatedAt
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
		Currency:         grpcJob.GetCurrency(),
		Salary:           salary,
		Requirements:     grpcJob.GetRequirements(),
		Description:      grpcJob.GetDescription(),
		Responsibilities: grpcJob.GetResponsibilities(),
		Benefits:         grpcJob.GetBenefits(),
	}
}

// All conversion functions updated to match new proto structure
