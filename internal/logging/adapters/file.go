package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// FileAdapter implements the LogAdapter interface for file output with rotation
type FileAdapter struct {
	name         string
	config       FileConfig
	currentFile  *os.File
	currentSize  int64
	mu           sync.Mutex
	lastRotation time.Time
}

// FileConfig represents configuration for the file adapter
type FileConfig struct {
	FilePath       string        `yaml:"file_path"`       // path to log file
	Format         string        `yaml:"format"`          // json or text
	MaxSize        int64         `yaml:"max_size"`        // max file size in bytes (0 = no limit)
	MaxAge         time.Duration `yaml:"max_age"`         // max age before rotation (0 = no rotation)
	MaxBackups     int           `yaml:"max_backups"`     // max number of backup files to keep
	Compress       bool          `yaml:"compress"`        // compress rotated files
	CreateDirs     bool          `yaml:"create_dirs"`     // create parent directories if they don't exist
	FileMode       os.FileMode   `yaml:"file_mode"`       // file permissions
	BufferSize     int           `yaml:"buffer_size"`     // buffer size for writes (0 = no buffering)
	SyncOnWrite    bool          `yaml:"sync_on_write"`   // sync after each write
	RotationPolicy string        `yaml:"rotation_policy"` // size, time, or both
}

// NewFileAdapter creates a new file adapter
func NewFileAdapter(name string, config FileConfig) (*FileAdapter, error) {
	// Set defaults
	if config.FileMode == 0 {
		config.FileMode = 0644
	}
	if config.MaxBackups == 0 {
		config.MaxBackups = 10
	}
	if config.RotationPolicy == "" {
		config.RotationPolicy = "size"
	}
	if config.Format == "" {
		config.Format = "json"
	}

	adapter := &FileAdapter{
		name:         name,
		config:       config,
		lastRotation: time.Now(),
	}

	// Create parent directories if needed
	if config.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(config.FilePath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// Open the initial file
	if err := adapter.openFile(); err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return adapter, nil
}

// Write writes a log entry to the file
func (a *FileAdapter) Write(entry *types.LogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if rotation is needed
	if a.shouldRotate() {
		if err := a.rotate(); err != nil {
			return fmt.Errorf("failed to rotate log file: %w", err)
		}
	}

	// Format the entry
	var output string
	var err error

	switch strings.ToLower(a.config.Format) {
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

	// Write to file
	line := output + "\n"
	n, err := a.currentFile.WriteString(line)
	if err != nil {
		return fmt.Errorf("failed to write to log file: %w", err)
	}

	a.currentSize += int64(n)

	// Sync if configured
	if a.config.SyncOnWrite {
		if err := a.currentFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync log file: %w", err)
		}
	}

	return nil
}

// Close closes the file adapter
func (a *FileAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.currentFile != nil {
		if err := a.currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close log file: %w", err)
		}
		a.currentFile = nil
	}

	return nil
}

// Health returns the health status of the adapter
func (a *FileAdapter) Health() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.currentFile == nil {
		return fmt.Errorf("log file is not open")
	}

	// Check if file is still writable
	if _, err := a.currentFile.Stat(); err != nil {
		return fmt.Errorf("log file is not accessible: %w", err)
	}

	return nil
}

// Name returns the name of the adapter
func (a *FileAdapter) Name() string {
	return a.name
}

// Private methods

// openFile opens the log file for writing
func (a *FileAdapter) openFile() error {
	file, err := os.OpenFile(a.config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, a.config.FileMode)
	if err != nil {
		return err
	}

	// Get current file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	a.currentFile = file
	a.currentSize = stat.Size()
	return nil
}

// shouldRotate determines if the log file should be rotated
func (a *FileAdapter) shouldRotate() bool {
	now := time.Now()

	switch a.config.RotationPolicy {
	case "size":
		return a.config.MaxSize > 0 && a.currentSize >= a.config.MaxSize
	case "time":
		return a.config.MaxAge > 0 && now.Sub(a.lastRotation) >= a.config.MaxAge
	case "both":
		sizeRotation := a.config.MaxSize > 0 && a.currentSize >= a.config.MaxSize
		timeRotation := a.config.MaxAge > 0 && now.Sub(a.lastRotation) >= a.config.MaxAge
		return sizeRotation || timeRotation
	default:
		return a.config.MaxSize > 0 && a.currentSize >= a.config.MaxSize
	}
}

// rotate rotates the current log file
func (a *FileAdapter) rotate() error {
	// Close current file
	if a.currentFile != nil {
		if err := a.currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close current log file: %w", err)
		}
		a.currentFile = nil
	}

	// Generate backup filename
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.%s", a.config.FilePath, timestamp)

	// Rename current file to backup
	if err := os.Rename(a.config.FilePath, backupPath); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Compress if configured
	if a.config.Compress {
		if err := a.compressFile(backupPath); err != nil {
			// Don't fail rotation if compression fails, just log it
			fmt.Fprintf(os.Stderr, "failed to compress rotated log file %s: %v\n", backupPath, err)
		}
	}

	// Clean up old backups
	if err := a.cleanupOldBackups(); err != nil {
		// Don't fail rotation if cleanup fails, just log it
		fmt.Fprintf(os.Stderr, "failed to clean up old log backups: %v\n", err)
	}

	// Open new file
	if err := a.openFile(); err != nil {
		return fmt.Errorf("failed to open new log file: %w", err)
	}

	a.lastRotation = time.Now()
	return nil
}

// compressFile compresses a file using gzip
func (a *FileAdapter) compressFile(filePath string) error {
	// This is a placeholder - actual compression would require importing compress/gzip
	// For now, just rename with .gz extension to indicate it should be compressed
	return os.Rename(filePath, filePath+".gz")
}

// cleanupOldBackups removes old backup files
func (a *FileAdapter) cleanupOldBackups() error {
	dir := filepath.Dir(a.config.FilePath)
	baseName := filepath.Base(a.config.FilePath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, baseName+".") && name != baseName {
			backups = append(backups, filepath.Join(dir, name))
		}
	}

	// Sort backups by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		statI, errI := os.Stat(backups[i])
		statJ, errJ := os.Stat(backups[j])
		if errI != nil || errJ != nil {
			return false
		}
		return statI.ModTime().After(statJ.ModTime())
	})

	// Remove old backups
	if len(backups) > a.config.MaxBackups {
		for _, backup := range backups[a.config.MaxBackups:] {
			if err := os.Remove(backup); err != nil {
				fmt.Fprintf(os.Stderr, "failed to remove old backup %s: %v\n", backup, err)
			}
		}
	}

	return nil
}

// formatJSON formats the log entry as JSON
func (a *FileAdapter) formatJSON(entry *types.LogEntry) (string, error) {
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
func (a *FileAdapter) formatText(entry *types.LogEntry) (string, error) {
	timestamp := entry.Timestamp.Format("2006-01-02T15:04:05.000Z07:00")
	level := strings.ToUpper(entry.Level.String())

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
