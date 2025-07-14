package logging

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// MultiLogger is the main implementation of the Logger interface
type MultiLogger struct {
	adapters map[string]types.LogAdapter
	level    LogLevel
	context  context.Context
	fields   map[string]interface{}
	mu       sync.RWMutex
}

// NewMultiLogger creates a new MultiLogger instance
func NewMultiLogger() *MultiLogger {
	return &MultiLogger{
		adapters: make(map[string]types.LogAdapter),
		level:    InfoLevel,
		context:  context.Background(),
		fields:   make(map[string]interface{}),
	}
}

// Debug logs a debug message
func (l *MultiLogger) Debug(message string, fields ...map[string]interface{}) {
	l.Log(DebugLevel, message, fields...)
}

// Info logs an info message
func (l *MultiLogger) Info(message string, fields ...map[string]interface{}) {
	l.Log(InfoLevel, message, fields...)
}

// Warn logs a warning message
func (l *MultiLogger) Warn(message string, fields ...map[string]interface{}) {
	l.Log(WarnLevel, message, fields...)
}

// Error logs an error message
func (l *MultiLogger) Error(message string, fields ...map[string]interface{}) {
	l.Log(ErrorLevel, message, fields...)
}

// Fatal logs a fatal message and exits
func (l *MultiLogger) Fatal(message string, fields ...map[string]interface{}) {
	l.Log(FatalLevel, message, fields...)
	l.Close()
	os.Exit(1)
}

// Log logs a message at the specified level
func (l *MultiLogger) Log(level LogLevel, message string, fields ...map[string]interface{}) {
	if level < l.level {
		return
	}

	entry := &types.LogEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
		Context:   l.context,
		Fields:    l.mergeFields(fields...),
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	// Write to all adapters
	for name, adapter := range l.adapters {
		if err := adapter.Write(entry); err != nil {
			// Log adapter errors to stderr to avoid infinite loops
			fmt.Fprintf(os.Stderr, "logging adapter %s error: %v\n", name, err)
		}
	}
}

// WithContext returns a new logger with the specified context
func (l *MultiLogger) WithContext(ctx context.Context) Logger {
	return &MultiLogger{
		adapters: l.adapters,
		level:    l.level,
		context:  ctx,
		fields:   l.copyFields(),
	}
}

// WithField returns a new logger with the specified field
func (l *MultiLogger) WithField(key string, value interface{}) Logger {
	fields := l.copyFields()
	fields[key] = value

	return &MultiLogger{
		adapters: l.adapters,
		level:    l.level,
		context:  l.context,
		fields:   fields,
	}
}

// WithFields returns a new logger with the specified fields
func (l *MultiLogger) WithFields(fields map[string]interface{}) Logger {
	mergedFields := l.copyFields()
	for k, v := range fields {
		mergedFields[k] = v
	}

	return &MultiLogger{
		adapters: l.adapters,
		level:    l.level,
		context:  l.context,
		fields:   mergedFields,
	}
}

// SetLevel sets the minimum log level
func (l *MultiLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *MultiLogger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// AddAdapter adds a new log adapter
func (l *MultiLogger) AddAdapter(adapter types.LogAdapter) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	name := adapter.Name()
	if _, exists := l.adapters[name]; exists {
		return fmt.Errorf("adapter %s already exists", name)
	}

	l.adapters[name] = adapter
	return nil
}

// RemoveAdapter removes a log adapter
func (l *MultiLogger) RemoveAdapter(adapterName string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	adapter, exists := l.adapters[adapterName]
	if !exists {
		return fmt.Errorf("adapter %s not found", adapterName)
	}

	if err := adapter.Close(); err != nil {
		return fmt.Errorf("failed to close adapter %s: %w", adapterName, err)
	}

	delete(l.adapters, adapterName)
	return nil
}

// Close closes all adapters
func (l *MultiLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errors []string
	for name, adapter := range l.adapters {
		if err := adapter.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("adapter %s: %v", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to close adapters: %s", strings.Join(errors, ", "))
	}

	return nil
}

// Helper methods

// copyFields creates a copy of the current fields map
func (l *MultiLogger) copyFields() map[string]interface{} {
	fields := make(map[string]interface{})
	for k, v := range l.fields {
		fields[k] = v
	}
	return fields
}

// mergeFields merges the logger's fields with additional fields
func (l *MultiLogger) mergeFields(additionalFields ...map[string]interface{}) map[string]interface{} {
	fields := l.copyFields()

	for _, fieldMap := range additionalFields {
		for k, v := range fieldMap {
			fields[k] = v
		}
	}

	return fields
}

// ParseLogLevel parses a string log level into LogLevel
func ParseLogLevel(levelStr string) LogLevel {
	switch strings.ToLower(levelStr) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn", "warning":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "fatal":
		return FatalLevel
	default:
		return InfoLevel
	}
}
