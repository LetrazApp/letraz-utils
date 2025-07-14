package types

import (
	"context"
	"time"
)

// LogLevel represents the severity level of a log entry
type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case FatalLevel:
		return "fatal"
	default:
		return "info"
	}
}

// LogEntry represents a single log entry
type LogEntry struct {
	Level     LogLevel               `json:"level"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Context   context.Context        `json:"-"`
}

// LogAdapter defines the interface for log output adapters
type LogAdapter interface {
	// Write writes a log entry to the adapter's destination
	Write(entry *LogEntry) error

	// Close closes the adapter and performs cleanup
	Close() error

	// Health returns the health status of the adapter
	Health() error

	// Name returns the name of the adapter
	Name() string
}

// Logger defines the main logging interface
type Logger interface {
	// Log methods for different levels
	Debug(message string, fields ...map[string]interface{})
	Info(message string, fields ...map[string]interface{})
	Warn(message string, fields ...map[string]interface{})
	Error(message string, fields ...map[string]interface{})
	Fatal(message string, fields ...map[string]interface{})

	// Contextual logging
	WithContext(ctx context.Context) Logger
	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger

	// Log entry creation
	Log(level LogLevel, message string, fields ...map[string]interface{})

	// Configuration
	SetLevel(level LogLevel)
	GetLevel() LogLevel

	// Adapter management
	AddAdapter(adapter LogAdapter) error
	RemoveAdapter(adapterName string) error

	// Cleanup
	Close() error
}

// AdapterConfig represents configuration for a specific adapter
type AdapterConfig struct {
	Name    string                 `yaml:"name"`
	Type    string                 `yaml:"type"`
	Enabled bool                   `yaml:"enabled"`
	Options map[string]interface{} `yaml:"options"`
}

// LoggerConfig represents the complete logging configuration
type LoggerConfig struct {
	Level    string          `yaml:"level"`
	Format   string          `yaml:"format"`
	Adapters []AdapterConfig `yaml:"adapters"`
}
