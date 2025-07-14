package logging

// Re-export types for backwards compatibility
import "letraz-utils/internal/logging/types"

type LogLevel = types.LogLevel
type LogEntry = types.LogEntry
type LogAdapter = types.LogAdapter
type Logger = types.Logger
type AdapterConfig = types.AdapterConfig
type LoggerConfig = types.LoggerConfig

// Re-export constants
const (
	DebugLevel = types.DebugLevel
	InfoLevel  = types.InfoLevel
	WarnLevel  = types.WarnLevel
	ErrorLevel = types.ErrorLevel
	FatalLevel = types.FatalLevel
)
