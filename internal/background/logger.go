package background

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"letraz-utils/internal/callback"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// TaskCompletionLogger handles structured logging for task completion
type TaskCompletionLogger struct {
	logger          types.Logger
	callbackClient  *callback.Client
	callbackEnabled bool
}

// NewTaskCompletionLogger creates a new task completion logger
func NewTaskCompletionLogger() *TaskCompletionLogger {
	return &TaskCompletionLogger{
		logger: logging.GetGlobalLogger(),
	}
}

// NewTaskCompletionLoggerWithCallback creates a new task completion logger with callback support
func NewTaskCompletionLoggerWithCallback(callbackClient *callback.Client, enabled bool) *TaskCompletionLogger {
	return &TaskCompletionLogger{
		logger:          logging.GetGlobalLogger(),
		callbackClient:  callbackClient,
		callbackEnabled: enabled,
	}
}

// TaskCompletionLog represents the structured log entry for task completion
type TaskCompletionLog struct {
	ProcessID      string                 `json:"processId"`
	Status         string                 `json:"status"`
	Data           interface{}            `json:"data,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	Operation      string                 `json:"operation"`
	ProcessingTime string                 `json:"processing_time"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// LogTaskCompletion logs task completion to stdout in structured JSON format
func (l *TaskCompletionLogger) LogTaskCompletion(result *TaskResult) error {
	// Create the structured log entry
	var processingTimeStr string
	if result.ProcessingTime != nil {
		processingTimeStr = result.ProcessingTime.String()
	} else {
		processingTimeStr = "0s"
	}

	logEntry := TaskCompletionLog{
		ProcessID:      result.ProcessID,
		Status:         string(result.Status),
		Data:           result.Data,
		Error:          result.Error,
		Timestamp:      time.Now(),
		Operation:      string(result.Type),
		ProcessingTime: processingTimeStr,
		Metadata:       result.Metadata,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		l.logger.Error("Failed to marshal task completion log", map[string]interface{}{
			"error": err.Error(),
		})
		return fmt.Errorf("failed to marshal task completion log: %w", err)
	}

	// Print to stdout (this will be captured by container orchestrators)
	fmt.Println(string(jsonData))

	// Also log to the application logger for debugging
	var processingTimeForLog interface{}
	if result.ProcessingTime != nil {
		processingTimeForLog = *result.ProcessingTime
	} else {
		processingTimeForLog = "not set"
	}

	l.logger.Info("Background task completed", map[string]interface{}{
		"process_id":      result.ProcessID,
		"status":          result.Status,
		"operation":       result.Type,
		"processing_time": processingTimeForLog,
	})

	// Send gRPC callback if enabled and client is available
	if l.callbackEnabled && l.callbackClient != nil {
		if err := l.sendTaskCallback(context.Background(), result); err != nil {
			l.logger.Error("Failed to send task callback", map[string]interface{}{
				"process_id": result.ProcessID,
				"error":      err.Error(),
			})
			// Don't return error here as logging succeeded, just callback failed
		}
	}

	return nil
}

// LogTaskStart logs when a task starts processing
func (l *TaskCompletionLogger) LogTaskStart(processID string, taskType TaskType) {
	l.logger.Info("Background task started", map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "PROCESSING",
	})
}

// LogTaskAccepted logs when a task is accepted for processing
func (l *TaskCompletionLogger) LogTaskAccepted(processID string, taskType TaskType) {
	l.logger.Info("Background task accepted", map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "ACCEPTED",
	})
}

// LogTaskError logs task errors during processing
func (l *TaskCompletionLogger) LogTaskError(processID string, taskType TaskType, err error) {
	l.logger.Error("Background task failed", map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "FAILURE",
		"error":      err.Error(),
	})
}

// LogTaskSuccess logs successful task completion
func (l *TaskCompletionLogger) LogTaskSuccess(processID string, taskType TaskType, processingTime time.Duration) {
	l.logger.Info("Background task completed successfully", map[string]interface{}{
		"process_id":      processID,
		"operation":       taskType,
		"status":          "SUCCESS",
		"processing_time": processingTime,
	})
}

// LogTaskMetrics logs task metrics for monitoring
func (l *TaskCompletionLogger) LogTaskMetrics(processID string, taskType TaskType, metrics map[string]interface{}) {
	logFields := map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"type":       "metrics",
	}

	// Add metrics to log fields
	for key, value := range metrics {
		logFields[key] = value
	}

	l.logger.Info("Background task metrics", logFields)
}

// CreateTaskCompletionLog creates a TaskCompletionLog from a TaskResult
func CreateTaskCompletionLog(result *TaskResult) *TaskCompletionLog {
	var processingTimeStr string
	if result.ProcessingTime != nil {
		processingTimeStr = result.ProcessingTime.String()
	} else {
		processingTimeStr = "0s"
	}

	return &TaskCompletionLog{
		ProcessID:      result.ProcessID,
		Status:         string(result.Status),
		Data:           result.Data,
		Error:          result.Error,
		Timestamp:      time.Now(),
		Operation:      string(result.Type),
		ProcessingTime: processingTimeStr,
		Metadata:       result.Metadata,
	}
}

// WriteStructuredLog writes a structured log entry directly to stdout
func WriteStructuredLog(logEntry interface{}) error {
	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	// Write to stdout
	_, err = os.Stdout.Write(append(jsonData, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write to stdout: %w", err)
	}

	return nil
}

// LogTaskCompletionToStdout logs task completion directly to stdout in the required format
func LogTaskCompletionToStdout(result *TaskResult) error {
	logEntry := CreateTaskCompletionLog(result)
	return WriteStructuredLog(logEntry)
}

// sendTaskCallback sends a task callback via gRPC
func (l *TaskCompletionLogger) sendTaskCallback(ctx context.Context, result *TaskResult) error {
	// Send callbacks for both scrape and tailor tasks
	switch result.Type {
	case TaskTypeScrape:
		return l.sendScrapeTaskCallback(ctx, result)
	case TaskTypeTailor:
		return l.sendTailorResumeTaskCallback(ctx, result)
	case TaskTypeScreenshot:
		return l.sendScreenshotTaskCallback(ctx, result)
	default:
		return nil
	}
}

// sendScrapeTaskCallback sends a scrape task callback via gRPC
func (l *TaskCompletionLogger) sendScrapeTaskCallback(ctx context.Context, result *TaskResult) error {

	// Create callback data from task result
	callbackData := &callback.CallbackData{
		ProcessID: result.ProcessID,
		Status:    string(result.Status),
		Timestamp: time.Now(),
		Operation: string(result.Type),
		ProcessingTime: func() time.Duration {
			if result.ProcessingTime != nil {
				return *result.ProcessingTime
			}
			return 0
		}(),
	}

	// Extract scrape-specific data if available
	if result.Data != nil {
		if scrapeData, ok := result.Data.(*ScrapeTaskData); ok {
			callbackData.Data = &callback.CallbackJobData{
				Job:     scrapeData.Job,
				Engine:  scrapeData.Engine,
				UsedLLM: scrapeData.UsedLLM,
			}
		}
	}

	// Extract metadata if available
	if result.Metadata != nil {
		callbackData.Metadata = &callback.CallbackMetadata{}

		if engine, ok := result.Metadata["engine"].(string); ok {
			callbackData.Metadata.Engine = engine
		}

		if url, ok := result.Metadata["url"].(string); ok {
			callbackData.Metadata.URL = url
		}
	}

	// Send the callback
	return l.callbackClient.SendScrapeJobCallback(ctx, callbackData)
}

// sendTailorResumeTaskCallback sends a TailorResume task callback via gRPC
func (l *TaskCompletionLogger) sendTailorResumeTaskCallback(ctx context.Context, result *TaskResult) error {
	// Create callback data from task result
	callbackData := &callback.TailorResumeCallbackData{
		ProcessID: result.ProcessID,
		Status:    string(result.Status),
		Timestamp: time.Now(),
		Operation: string(result.Type),
		ProcessingTime: func() time.Duration {
			if result.ProcessingTime != nil {
				return *result.ProcessingTime
			}
			return 0
		}(),
	}

	// Extract TailorResume-specific data if available
	if result.Data != nil {
		if tailorData, ok := result.Data.(*TailorTaskData); ok {
			callbackData.Data = &callback.TailorResumeJobData{
				TailoredResume: tailorData.TailoredResume,
				Suggestions:    tailorData.Suggestions,
				ThreadID:       tailorData.ThreadID,
			}
		}
	}

	// Extract metadata if available
	if result.Metadata != nil {
		callbackData.Metadata = &callback.TailorResumeCallbackMetadata{}

		if company, ok := result.Metadata["company"].(string); ok {
			callbackData.Metadata.Company = company
		}

		if jobTitle, ok := result.Metadata["job_title"].(string); ok {
			callbackData.Metadata.JobTitle = jobTitle
		}

		if resumeID, ok := result.Metadata["resume_id"].(string); ok {
			callbackData.Metadata.ResumeID = resumeID
		}
	}

	// Send the callback
	return l.callbackClient.SendTailorResumeCallback(ctx, callbackData)
}

// sendScreenshotTaskCallback sends a screenshot task callback via gRPC
func (l *TaskCompletionLogger) sendScreenshotTaskCallback(ctx context.Context, result *TaskResult) error {
	// Create callback data from task result
	callbackData := &callback.ScreenshotCallbackData{
		ProcessID: result.ProcessID,
		Status:    string(result.Status),
		Timestamp: time.Now(),
		Operation: string(result.Type),
		ProcessingTime: func() time.Duration {
			if result.ProcessingTime != nil {
				return *result.ProcessingTime
			}
			return 0
		}(),
	}

	// Extract screenshot-specific data if available
	if result.Data != nil {
		if screenshotData, ok := result.Data.(*ScreenshotTaskData); ok {
			callbackData.Data = &callback.ScreenshotJobData{
				ScreenshotURL: screenshotData.ScreenshotURL,
				ResumeID:      screenshotData.ResumeID,
				FileSizeBytes: screenshotData.FileSize,
			}
		}
	}

	// Extract metadata if available
	if result.Metadata != nil {
		callbackData.Metadata = &callback.ScreenshotCallbackMetadata{}

		if resumeID, ok := result.Metadata["resume_id"].(string); ok {
			callbackData.Metadata.ResumeID = resumeID
		}
		if screenshotURL, ok := result.Metadata["screenshot_url"].(string); ok {
			callbackData.Metadata.ScreenshotURL = screenshotURL
		}
		if fileSize, ok := result.Metadata["file_size"].(int); ok {
			callbackData.Metadata.FileSize = fileSize
		}
		if fileSizeFloat, ok := result.Metadata["file_size"].(float64); ok {
			callbackData.Metadata.FileSize = int(fileSizeFloat)
		}
	}

	// Send the callback
	return l.callbackClient.SendGenerateScreenshotCallback(ctx, callbackData)
}
