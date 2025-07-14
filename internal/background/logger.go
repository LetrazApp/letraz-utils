package background

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// TaskCompletionLogger handles structured logging for task completion
type TaskCompletionLogger struct {
	logger types.Logger
}

// NewTaskCompletionLogger creates a new task completion logger
func NewTaskCompletionLogger() *TaskCompletionLogger {
	return &TaskCompletionLogger{
		logger: logging.GetGlobalLogger(),
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
