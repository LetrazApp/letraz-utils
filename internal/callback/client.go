package callback

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/structpb"

	letrazv1 "letraz-utils/api/proto/letraz/v1"
	"letraz-utils/internal/logging"
	"letraz-utils/pkg/models"
)

// Client represents a gRPC client for making callbacks
type Client struct {
	conn               *grpc.ClientConn
	scrapeClient       letrazv1.ScrapeJobCallbackControllerClient
	tailorResumeClient letrazv1.TailorResumeCallBackControllerClient
	screenshotClient   letrazv1.GenerateScreenshotCallBackControllerClient
	logger             logging.Logger
}

// ClientConfig holds configuration for the callback client
type ClientConfig struct {
	ServerAddress string        `yaml:"server_address"`
	Timeout       time.Duration `yaml:"timeout"`
	MaxRetries    int           `yaml:"max_retries"`
}

// NewClient creates a new callback gRPC client
func NewClient(config *ClientConfig, logger logging.Logger) (*Client, error) {
	if config.ServerAddress == "" {
		return nil, fmt.Errorf("server address is required")
	}

	// Set default timeout if not provided
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Set default max retries if not provided
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	// Determine connection parameters
	serverAddr, creds := determineConnectionParams(config.ServerAddress, logger)

	// Create gRPC connection with proper credentials and connection options
	conn, err := grpc.NewClient(
		serverAddr,
		grpc.WithTransportCredentials(creds),
		// Add keepalive parameters for better connection stability
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		// Use custom dialer to prefer IPv4
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{
				Timeout: config.Timeout,
				// Prefer IPv4 to avoid IPv6 routing issues
				FallbackDelay: 0,
			}).DialContext(ctx, "tcp4", addr)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", serverAddr, err)
	}

	// Create clients
	scrapeClient := letrazv1.NewScrapeJobCallbackControllerClient(conn)
	tailorResumeClient := letrazv1.NewTailorResumeCallBackControllerClient(conn)
	screenshotClient := letrazv1.NewGenerateScreenshotCallBackControllerClient(conn)

	return &Client{
		conn:               conn,
		scrapeClient:       scrapeClient,
		tailorResumeClient: tailorResumeClient,
		screenshotClient:   screenshotClient,
		logger:             logger,
	}, nil
}

// Close closes the gRPC connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SendScrapeJobCallback sends a scrape job callback to the server
func (c *Client) SendScrapeJobCallback(ctx context.Context, result *CallbackData) error {
	req := convertToCallbackRequest(result)

	c.logger.Info("Sending scrape job callback", map[string]interface{}{
		"process_id": req.ProcessId,
		"status":     req.Status,
		"operation":  req.Operation,
	})

	// Create context with timeout
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Make the gRPC call
	response, err := c.scrapeClient.ScrapeJobCallBack(callCtx, req)
	if err != nil {
		c.logger.Error("Failed to send scrape job callback", map[string]interface{}{
			"process_id": req.ProcessId,
			"error":      err.Error(),
		})
		return fmt.Errorf("failed to send callback: %w", err)
	}

	// Log success with response message if available
	logFields := map[string]interface{}{
		"process_id": req.ProcessId,
	}
	if response != nil && response.Msg != nil {
		logFields["response_msg"] = *response.Msg
	}

	c.logger.Info("Scrape job callback sent successfully", logFields)

	return nil
}

// SendTailorResumeCallback sends a TailorResume callback to the server
func (c *Client) SendTailorResumeCallback(ctx context.Context, result *TailorResumeCallbackData) error {
	req := convertToTailorResumeCallbackRequest(result)

	c.logger.Info("Sending TailorResume callback", map[string]interface{}{
		"process_id":   req.ProcessId,
		"status":       req.Status,
		"operation":    req.Operation,
		"method_name":  "/letraz_server.RESUME.TailorResumeCallBackController/TailorResumeCallBack",
		"client_state": c.conn.GetState().String(),
		"target":       c.conn.Target(),
	})

	// Create context with timeout
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Make the gRPC call
	response, err := c.tailorResumeClient.TailorResumeCallBack(callCtx, req)
	if err != nil {
		c.logger.Error("Failed to send TailorResume callback", map[string]interface{}{
			"process_id": req.ProcessId,
			"error":      err.Error(),
		})
		return fmt.Errorf("failed to send TailorResume callback: %w", err)
	}

	// Log success with response message if available
	logFields := map[string]interface{}{
		"process_id": req.ProcessId,
	}
	if response != nil && response.Msg != nil {
		logFields["response_msg"] = *response.Msg
	}

	c.logger.Info("TailorResume callback sent successfully", logFields)

	return nil
}

// SendGenerateScreenshotCallback sends a GenerateScreenshot callback to the server
func (c *Client) SendGenerateScreenshotCallback(ctx context.Context, result *ScreenshotCallbackData) error {
	req := convertToScreenshotCallbackRequest(result)

	c.logger.Info("Sending GenerateScreenshot callback", map[string]interface{}{
		"process_id":   req.ProcessId,
		"status":       req.Status,
		"operation":    req.Operation,
		"method_name":  "/letraz_server.RESUME.GenerateScreenshotCallBackController/GenerateScreenshotCallBack",
		"client_state": c.conn.GetState().String(),
		"target":       c.conn.Target(),
	})

	// Create context with timeout
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Make the gRPC call
	response, err := c.screenshotClient.GenerateScreenshotCallBack(callCtx, req)
	if err != nil {
		c.logger.Error("Failed to send GenerateScreenshot callback", map[string]interface{}{
			"process_id": req.ProcessId,
			"error":      err.Error(),
		})
		return fmt.Errorf("failed to send GenerateScreenshot callback: %w", err)
	}

	// Log success with response message if available
	logFields := map[string]interface{}{
		"process_id": req.ProcessId,
	}
	if response != nil && response.Msg != nil {
		logFields["response_msg"] = *response.Msg
	}

	c.logger.Info("GenerateScreenshot callback sent successfully", logFields)

	return nil
}

// CallbackData represents the data structure for callbacks
type CallbackData struct {
	ProcessID      string
	Status         string
	Data           *CallbackJobData
	Timestamp      time.Time
	Operation      string
	ProcessingTime time.Duration
	Metadata       *CallbackMetadata
}

// CallbackJobData represents job data for callbacks
type CallbackJobData struct {
	Job     *models.Job
	Engine  string
	UsedLLM bool
}

// CallbackMetadata represents metadata for callbacks
type CallbackMetadata struct {
	Engine string
	URL    string
}

// TailorResumeCallbackData represents the data structure for TailorResume callbacks
type TailorResumeCallbackData struct {
	ProcessID      string
	Status         string
	Data           *TailorResumeJobData
	Timestamp      time.Time
	Operation      string
	ProcessingTime time.Duration
	Metadata       *TailorResumeCallbackMetadata
}

// TailorResumeJobData represents TailorResume job data for callbacks
type TailorResumeJobData struct {
	TailoredResume *models.TailoredResume
	Suggestions    []models.Suggestion
	ThreadID       string
}

// TailorResumeCallbackMetadata represents metadata for TailorResume callbacks
type TailorResumeCallbackMetadata struct {
	Company  string
	JobTitle string
	ResumeID string
}

// convertToCallbackRequest converts CallbackData to the gRPC request format
func convertToCallbackRequest(data *CallbackData) *letrazv1.ScrapeJobCallbackRequest {
	req := &letrazv1.ScrapeJobCallbackRequest{
		ProcessId:      data.ProcessID,
		Status:         data.Status,
		Timestamp:      data.Timestamp.Format(time.RFC3339Nano),
		Operation:      data.Operation,
		ProcessingTime: data.ProcessingTime.String(),
	}

	// Convert job data based on status
	// For failure callbacks, explicitly set data to null for clear semantics
	// For success callbacks, populate with actual job data if available
	if isFailureStatus(data.Status) {
		// Explicitly set data to nil for failures - this serializes as null in JSON
		req.Data = nil
	} else if data.Data != nil && hasValidJobData(data.Data) {
		// Only include data for successful operations with meaningful content
		req.Data = &letrazv1.ScrapeJobDataRequest{
			Engine:  &data.Data.Engine,
			UsedLlm: &data.Data.UsedLLM,
		}

		// Convert job details if available
		if data.Data.Job != nil {
			job := data.Data.Job
			req.Data.Job = &letrazv1.JobDetailRequest{
				Title:            job.Title,
				JobUrl:           job.JobURL,
				CompanyName:      job.CompanyName,
				Location:         job.Location,
				Requirements:     job.Requirements,
				Description:      job.Description,
				Responsibilities: job.Responsibilities,
				Benefits:         job.Benefits,
			}

			// Convert salary if available
			if job.Salary.Currency != "" || job.Salary.Max > 0 || job.Salary.Min > 0 {
				req.Data.Job.Salary = &letrazv1.JobSalaryRequest{
					Currency: &job.Salary.Currency,
					Max:      func() *int32 { v := int32(job.Salary.Max); return &v }(),
					Min:      func() *int32 { v := int32(job.Salary.Min); return &v }(),
				}
			}
		}
	} else {
		// For edge cases (no data available even on success), set to nil
		req.Data = nil
	}

	// Convert metadata if available
	if data.Metadata != nil {
		req.Metadata = &letrazv1.CallbackMetadataRequest{
			Engine: &data.Metadata.Engine,
			Url:    &data.Metadata.URL,
		}
	}

	return req
}

// convertToTailorResumeCallbackRequest converts TailorResumeCallbackData to the gRPC request format
func convertToTailorResumeCallbackRequest(data *TailorResumeCallbackData) *letrazv1.TailorResumeCallBackRequest {
	req := &letrazv1.TailorResumeCallBackRequest{
		ProcessId:      data.ProcessID,
		Status:         data.Status,
		Timestamp:      data.Timestamp.Format(time.RFC3339Nano),
		Operation:      data.Operation,
		ProcessingTime: data.ProcessingTime.String(),
	}

	// Convert TailorResume data if available
	if data.Data != nil {
		req.Data = &letrazv1.DataRequest{ThreadId: data.Data.ThreadID}

		// Convert TailoredResume if available
		if data.Data.TailoredResume != nil {
			resume := data.Data.TailoredResume
			req.Data.TailoredResume = &letrazv1.TailoredResumeRequest{
				Id: resume.ID,
			}

			// Convert sections
			if len(resume.Sections) > 0 {
				sections := make([]*letrazv1.SectionRequest, len(resume.Sections))
				for i, section := range resume.Sections {
					// Convert data to protobuf.Struct
					var protoStruct *structpb.Struct
					if section.Data != nil {
						if structData, err := structpb.NewStruct(convertToMap(section.Data)); err == nil {
							protoStruct = structData
						}
					}

					sections[i] = &letrazv1.SectionRequest{
						Type: section.Type,
						Data: protoStruct,
					}
				}
				req.Data.TailoredResume.Sections = sections
			}
		}

		// Convert suggestions if available
		if len(data.Data.Suggestions) > 0 {
			suggestions := make([]*letrazv1.SuggestionRequest, len(data.Data.Suggestions))
			for i, suggestion := range data.Data.Suggestions {
				suggestions[i] = &letrazv1.SuggestionRequest{
					Id:        suggestion.ID,
					Type:      suggestion.Type,
					Priority:  suggestion.Priority,
					Impact:    suggestion.Impact,
					Section:   suggestion.Section,
					Current:   suggestion.Current,
					Suggested: suggestion.Suggested,
					Reasoning: suggestion.Reasoning,
				}
			}
			req.Data.Suggestions = suggestions
		}
	}

	// Convert metadata if available
	if data.Metadata != nil {
		req.Metadata = &letrazv1.MetadataRequest{
			Company:  data.Metadata.Company,
			JobTitle: data.Metadata.JobTitle,
			ResumeId: data.Metadata.ResumeID,
		}
	}

	return req
}

// ScreenshotCallbackData represents the data structure for GenerateScreenshot callbacks
type ScreenshotCallbackData struct {
	ProcessID      string
	Status         string
	Data           *ScreenshotJobData
	Timestamp      time.Time
	Operation      string
	ProcessingTime time.Duration
	Metadata       *ScreenshotCallbackMetadata
}

// ScreenshotJobData represents screenshot job data for callbacks
type ScreenshotJobData struct {
	ScreenshotURL string
	ResumeID      string
	FileSizeBytes int
}

// ScreenshotCallbackMetadata represents metadata for screenshot callbacks
type ScreenshotCallbackMetadata struct {
	FileSize      int
	ResumeID      string
	ScreenshotURL string
}

// convertToScreenshotCallbackRequest converts ScreenshotCallbackData to the gRPC request format
func convertToScreenshotCallbackRequest(data *ScreenshotCallbackData) *letrazv1.GenerateScreenshotCallBackRequest {
	req := &letrazv1.GenerateScreenshotCallBackRequest{
		ProcessId:      data.ProcessID,
		Status:         data.Status,
		Timestamp:      data.Timestamp.Format(time.RFC3339Nano),
		Operation:      data.Operation,
		ProcessingTime: data.ProcessingTime.String(),
	}

	if data.Data != nil {
		req.Data = &letrazv1.ScreenshotDataRequest{
			ScreenshotUrl: data.Data.ScreenshotURL,
			ResumeId:      data.Data.ResumeID,
			FileSizeBytes: int32(data.Data.FileSizeBytes),
		}
	}

	if data.Metadata != nil {
		req.Metadata = &letrazv1.ScreenshotMetadataRequest{
			FileSize:      int32(data.Metadata.FileSize),
			ResumeId:      data.Metadata.ResumeID,
			ScreenshotUrl: data.Metadata.ScreenshotURL,
		}
	}

	return req
}

// convertToMap converts interface{} to map[string]interface{} for structpb conversion
func convertToMap(data interface{}) map[string]interface{} {
	if dataMap, ok := data.(map[string]interface{}); ok {
		return dataMap
	}

	// Try to convert via JSON for other types
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return map[string]interface{}{}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return map[string]interface{}{}
	}

	return result
}

// determineConnectionParams analyzes the server address and returns appropriate connection parameters
func determineConnectionParams(serverAddress string, logger logging.Logger) (string, credentials.TransportCredentials) {
	// Check if it's a localhost address
	if isLocalhost(serverAddress) {
		// For localhost, use insecure connection and default to port 9090 if no port specified
		addr := ensurePort(serverAddress, "9090")
		logger.Info("Using insecure connection for localhost", map[string]interface{}{
			"address": addr,
		})
		return addr, insecure.NewCredentials()
	}

	// Check if it's an ngrok or other external domain
	if isExternalDomain(serverAddress) {
		// For external domains (like ngrok), use TLS and default to port 443
		addr := ensurePort(serverAddress, "443")
		logger.Info("Using TLS connection for external domain", map[string]interface{}{
			"address": addr,
		})
		return addr, credentials.NewTLS(nil)
	}

	// Default: assume it's an external address with TLS
	addr := ensurePort(serverAddress, "443")
	logger.Info("Using TLS connection (default)", map[string]interface{}{
		"address": addr,
	})
	return addr, credentials.NewTLS(nil)
}

// isLocalhost checks if the address is localhost/127.0.0.1
func isLocalhost(addr string) bool {
	// Remove port if present for checking
	host := strings.Split(addr, ":")[0]
	return host == "localhost" || host == "127.0.0.1"
}

// isExternalDomain checks if the address looks like an external domain
func isExternalDomain(addr string) bool {
	// Remove port if present for checking
	host := strings.Split(addr, ":")[0]

	// Check for ngrok domains
	if strings.Contains(host, "ngrok") {
		return true
	}

	// Check if it contains dots (likely a domain)
	return strings.Contains(host, ".")
}

// ensurePort adds a default port to the address if no port is specified
func ensurePort(addr, defaultPort string) string {
	// If already has port, return as-is
	if strings.Contains(addr, ":") {
		return addr
	}

	// Add default port
	return fmt.Sprintf("%s:%s", addr, defaultPort)
}

// isFailureStatus checks if the callback status indicates a failure
// For failures, we explicitly set the data field to null for clear semantics
func isFailureStatus(status string) bool {
	return strings.EqualFold(status, "failure") ||
		strings.EqualFold(status, "failed") ||
		strings.EqualFold(status, "error")
}

// hasValidJobData checks if the job data contains meaningful content worth sending
// This helps distinguish between successful operations with data vs. those without meaningful content
func hasValidJobData(data *CallbackJobData) bool {
	if data == nil {
		return false
	}

	// If there's no job, but we have engine/LLM info, still consider it valid
	// as this metadata might be useful for debugging
	if data.Job == nil {
		return data.Engine != ""
	}

	// Check if the job has meaningful content
	job := data.Job
	return job.Title != "" ||
		job.CompanyName != "" ||
		job.Description != "" ||
		len(job.Requirements) > 0 ||
		len(job.Responsibilities) > 0
}
