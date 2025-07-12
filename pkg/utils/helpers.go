package utils

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateRequestID generates a unique request ID for tracking
func GenerateRequestID() string {
	return uuid.New().String()
}

// FormatDuration formats a duration to a human-readable string
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// Contains checks if a string slice contains a specific string
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetStringOrDefault returns the value if not empty, otherwise returns the default
func GetStringOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// FindRegexMatch finds the first match of a regex pattern in text
func FindRegexMatch(text, pattern string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re.FindStringSubmatch(text)
}

// ExtractDomainFromURL extracts the domain from a URL
func ExtractDomainFromURL(url string) string {
	// Remove protocol
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	// Find the first slash or end of string
	if slashIndex := strings.Index(url, "/"); slashIndex != -1 {
		url = url[:slashIndex]
	}

	// Remove port if present
	if colonIndex := strings.Index(url, ":"); colonIndex != -1 {
		url = url[:colonIndex]
	}

	return url
}

// GenerateProcessID generates a unique process ID for background tasks
func GenerateProcessID() string {
	return uuid.New().String()
}

// GenerateProcessIDWithPrefix generates a unique process ID with a type prefix
func GenerateProcessIDWithPrefix(taskType string) string {
	timestamp := time.Now().Format("20060102")
	return fmt.Sprintf("%s_%s_%s", taskType, timestamp, uuid.New().String())
}

// GenerateScrapeProcessID generates a unique process ID for scrape tasks
func GenerateScrapeProcessID() string {
	return GenerateProcessIDWithPrefix("scrape")
}

// GenerateTailorProcessID generates a unique process ID for tailor tasks
func GenerateTailorProcessID() string {
	return GenerateProcessIDWithPrefix("tailor")
}

// IsValidProcessID validates if a string is a valid process ID format
func IsValidProcessID(processID string) bool {
	if processID == "" {
		return false
	}

	// Check for UUID format (basic validation)
	if _, err := uuid.Parse(processID); err == nil {
		return true
	}

	// Check for prefixed format (tasktype_timestamp_uuid)
	parts := strings.Split(processID, "_")
	if len(parts) >= 3 {
		// Last part should be a valid UUID
		lastPart := parts[len(parts)-1]
		if _, err := uuid.Parse(lastPart); err == nil {
			return true
		}
	}

	return false
}

// ExtractTaskTypeFromProcessID extracts the task type from a prefixed process ID
func ExtractTaskTypeFromProcessID(processID string) string {
	if !IsValidProcessID(processID) {
		return ""
	}

	parts := strings.Split(processID, "_")
	if len(parts) >= 3 {
		return parts[0]
	}

	return ""
}

// IsProcessIDForTaskType checks if a process ID is for a specific task type
func IsProcessIDForTaskType(processID, taskType string) bool {
	extractedType := ExtractTaskTypeFromProcessID(processID)
	return extractedType == taskType
}

// ProcessIDMetadata represents metadata extracted from a process ID
type ProcessIDMetadata struct {
	TaskType  string
	Timestamp string
	UUID      string
	IsValid   bool
}

// ParseProcessID parses a process ID and extracts metadata
func ParseProcessID(processID string) *ProcessIDMetadata {
	metadata := &ProcessIDMetadata{
		IsValid: IsValidProcessID(processID),
	}

	if !metadata.IsValid {
		return metadata
	}

	// Try to parse prefixed format first
	parts := strings.Split(processID, "_")
	if len(parts) >= 3 {
		metadata.TaskType = parts[0]
		metadata.Timestamp = parts[1]
		metadata.UUID = parts[len(parts)-1]
	} else {
		// Plain UUID format
		metadata.UUID = processID
	}

	return metadata
}

// IsDevelopment checks if the application is running in development mode
func IsDevelopment() bool {
	env := os.Getenv("GO_ENV")
	return env == "development" || env == "dev" || env == ""
}
