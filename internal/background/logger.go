package background

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"letraz-utils/pkg/utils"
)

// TaskCompletionLogger handles structured logging for task completion
type TaskCompletionLogger struct {
	logger *logrus.Logger
}

// NewTaskCompletionLogger creates a new task completion logger
func NewTaskCompletionLogger() *TaskCompletionLogger {
	return &TaskCompletionLogger{
		logger: utils.GetLogger(),
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
	logEntry := TaskCompletionLog{
		ProcessID:      result.ProcessID,
		Status:         string(result.Status),
		Data:           result.Data,
		Error:          result.Error,
		Timestamp:      time.Now(),
		Operation:      string(result.Type),
		ProcessingTime: result.ProcessingTime.String(),
		Metadata:       result.Metadata,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		l.logger.WithError(err).Error("Failed to marshal task completion log")
		return fmt.Errorf("failed to marshal task completion log: %w", err)
	}

	// Print to stdout (this will be captured by container orchestrators)
	fmt.Println(string(jsonData))

	// Also log to the application logger for debugging
	l.logger.WithFields(map[string]interface{}{
		"process_id":      result.ProcessID,
		"status":          result.Status,
		"operation":       result.Type,
		"processing_time": result.ProcessingTime,
	}).Info("Background task completed")

	return nil
}

// LogTaskStart logs when a task starts processing
func (l *TaskCompletionLogger) LogTaskStart(processID string, taskType TaskType) {
	l.logger.WithFields(map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "PROCESSING",
	}).Info("Background task started")
}

// LogTaskAccepted logs when a task is accepted for processing
func (l *TaskCompletionLogger) LogTaskAccepted(processID string, taskType TaskType) {
	l.logger.WithFields(map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "ACCEPTED",
	}).Info("Background task accepted")
}

// LogTaskError logs task errors during processing
func (l *TaskCompletionLogger) LogTaskError(processID string, taskType TaskType, err error) {
	l.logger.WithFields(map[string]interface{}{
		"process_id": processID,
		"operation":  taskType,
		"status":     "FAILURE",
		"error":      err.Error(),
	}).Error("Background task failed")
}

// LogTaskSuccess logs successful task completion
func (l *TaskCompletionLogger) LogTaskSuccess(processID string, taskType TaskType, processingTime time.Duration) {
	l.logger.WithFields(map[string]interface{}{
		"process_id":      processID,
		"operation":       taskType,
		"status":          "SUCCESS",
		"processing_time": processingTime,
	}).Info("Background task completed successfully")
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

	l.logger.WithFields(logFields).Info("Background task metrics")
}

// CreateTaskCompletionLog creates a TaskCompletionLog from a TaskResult
func CreateTaskCompletionLog(result *TaskResult) *TaskCompletionLog {
	return &TaskCompletionLog{
		ProcessID:      result.ProcessID,
		Status:         string(result.Status),
		Data:           result.Data,
		Error:          result.Error,
		Timestamp:      time.Now(),
		Operation:      string(result.Type),
		ProcessingTime: result.ProcessingTime.String(),
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
