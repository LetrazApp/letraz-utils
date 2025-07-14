package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// StdoutAdapter implements the LogAdapter interface for stdout output
type StdoutAdapter struct {
	name      string
	format    string
	colorized bool
	mu        sync.Mutex
}

// StdoutConfig represents configuration for the stdout adapter
type StdoutConfig struct {
	Format    string `yaml:"format"`    // json or text
	Colorized bool   `yaml:"colorized"` // enable colored output
}

// NewStdoutAdapter creates a new stdout adapter
func NewStdoutAdapter(name string, config StdoutConfig) *StdoutAdapter {
	return &StdoutAdapter{
		name:      name,
		format:    config.Format,
		colorized: config.Colorized,
	}
}

// Write writes a log entry to stdout
func (a *StdoutAdapter) Write(entry *types.LogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var output string
	var err error

	switch strings.ToLower(a.format) {
	case "json":
		output, err = a.formatJSON(entry)
	case "text":
		output, err = a.formatText(entry)
	default:
		output, err = a.formatJSON(entry)
	}

	if err != nil {
		return fmt.Errorf("failed to format log entry: %w", err)
	}

	_, err = fmt.Fprintln(os.Stdout, output)
	return err
}

// Close closes the adapter (no-op for stdout)
func (a *StdoutAdapter) Close() error {
	return nil
}

// Health returns the health status of the adapter
func (a *StdoutAdapter) Health() error {
	return nil // stdout is always healthy
}

// Name returns the name of the adapter
func (a *StdoutAdapter) Name() string {
	return a.name
}

// formatJSON formats the log entry as JSON
func (a *StdoutAdapter) formatJSON(entry *types.LogEntry) (string, error) {
	logData := map[string]interface{}{
		"level":   entry.Level.String(),
		"message": entry.Message,
		"time":    entry.Timestamp.Format(time.RFC3339),
	}

	// Add fields if they exist
	if len(entry.Fields) > 0 {
		for k, v := range entry.Fields {
			logData[k] = v
		}
	}

	data, err := json.Marshal(logData)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// formatText formats the log entry as human-readable text
func (a *StdoutAdapter) formatText(entry *types.LogEntry) (string, error) {
	timestamp := entry.Timestamp.Format("2006-01-02T15:04:05.000Z07:00")
	level := strings.ToUpper(entry.Level.String())

	// Apply color if enabled
	if a.colorized {
		level = a.colorizeLevel(level)
	}

	output := fmt.Sprintf("%s [%s] %s", timestamp, level, entry.Message)

	// Add fields if they exist
	if len(entry.Fields) > 0 {
		var fields []string
		for k, v := range entry.Fields {
			fields = append(fields, fmt.Sprintf("%s=%v", k, v))
		}
		output += " " + strings.Join(fields, " ")
	}

	return output, nil
}

// colorizeLevel adds ANSI color codes to log levels
func (a *StdoutAdapter) colorizeLevel(level string) string {
	const (
		red    = "\033[31m"
		yellow = "\033[33m"
		blue   = "\033[34m"
		gray   = "\033[90m"
		reset  = "\033[0m"
	)

	switch level {
	case "DEBUG":
		return gray + level + reset
	case "INFO":
		return blue + level + reset
	case "WARN":
		return yellow + level + reset
	case "ERROR", "FATAL":
		return red + level + reset
	default:
		return level
	}
}
