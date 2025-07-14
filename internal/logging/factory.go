package logging

import (
	"fmt"
	"time"

	"letraz-utils/internal/logging/adapters"
	"letraz-utils/internal/logging/types"
)

// AdapterFactory creates logging adapters based on configuration
type AdapterFactory struct{}

// NewAdapterFactory creates a new adapter factory
func NewAdapterFactory() *AdapterFactory {
	return &AdapterFactory{}
}

// CreateAdapter creates a logging adapter based on the provided configuration
func (f *AdapterFactory) CreateAdapter(adapterConfig types.AdapterConfig) (types.LogAdapter, error) {
	switch adapterConfig.Type {
	case "stdout":
		return f.createStdoutAdapter(adapterConfig)
	case "file":
		return f.createFileAdapter(adapterConfig)
	case "betterstack":
		return f.createBetterstackAdapter(adapterConfig)
	default:
		return nil, fmt.Errorf("unsupported adapter type: %s", adapterConfig.Type)
	}
}

// createStdoutAdapter creates a stdout adapter
func (f *AdapterFactory) createStdoutAdapter(adapterConfig types.AdapterConfig) (types.LogAdapter, error) {
	config := adapters.StdoutConfig{
		Format:    getStringOption(adapterConfig.Options, "format", "json"),
		Colorized: getBoolOption(adapterConfig.Options, "colorized", false),
	}

	return adapters.NewStdoutAdapter(adapterConfig.Name, config), nil
}

// createFileAdapter creates a file adapter
func (f *AdapterFactory) createFileAdapter(adapterConfig types.AdapterConfig) (types.LogAdapter, error) {
	config := adapters.FileConfig{
		FilePath:       getStringOption(adapterConfig.Options, "file_path", ""),
		Format:         getStringOption(adapterConfig.Options, "format", "json"),
		MaxSize:        getInt64Option(adapterConfig.Options, "max_size", 0),
		MaxAge:         getDurationOption(adapterConfig.Options, "max_age", 0),
		MaxBackups:     getIntOption(adapterConfig.Options, "max_backups", 10),
		Compress:       getBoolOption(adapterConfig.Options, "compress", false),
		CreateDirs:     getBoolOption(adapterConfig.Options, "create_dirs", true),
		FileMode:       0644,
		BufferSize:     getIntOption(adapterConfig.Options, "buffer_size", 0),
		SyncOnWrite:    getBoolOption(adapterConfig.Options, "sync_on_write", false),
		RotationPolicy: getStringOption(adapterConfig.Options, "rotation_policy", "size"),
	}

	if config.FilePath == "" {
		return nil, fmt.Errorf("file_path is required for file adapter")
	}

	return adapters.NewFileAdapter(adapterConfig.Name, config)
}

// createBetterstackAdapter creates a Betterstack adapter
func (f *AdapterFactory) createBetterstackAdapter(adapterConfig types.AdapterConfig) (types.LogAdapter, error) {
	config := adapters.BetterstackConfig{
		SourceToken:   getStringOption(adapterConfig.Options, "source_token", ""),
		Endpoint:      getStringOption(adapterConfig.Options, "endpoint", "https://in.logs.betterstack.com"),
		BatchSize:     getIntOption(adapterConfig.Options, "batch_size", 100),
		FlushInterval: getDurationOption(adapterConfig.Options, "flush_interval", 5*time.Second),
		MaxRetries:    getIntOption(adapterConfig.Options, "max_retries", 3),
		Timeout:       getDurationOption(adapterConfig.Options, "timeout", 30*time.Second),
		UserAgent:     getStringOption(adapterConfig.Options, "user_agent", "letraz-utils/1.0"),
		Headers:       getMapStringOption(adapterConfig.Options, "headers"),
	}

	if config.SourceToken == "" {
		return nil, fmt.Errorf("source_token is required for Betterstack adapter")
	}

	return adapters.NewBetterstackAdapter(adapterConfig.Name, config)
}

// Helper functions to extract options with defaults

func getStringOption(options map[string]interface{}, key string, defaultValue string) string {
	if value, exists := options[key]; exists {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

func getIntOption(options map[string]interface{}, key string, defaultValue int) int {
	if value, exists := options[key]; exists {
		if intVal, ok := value.(int); ok {
			return intVal
		}
		if floatVal, ok := value.(float64); ok {
			return int(floatVal)
		}
	}
	return defaultValue
}

func getInt64Option(options map[string]interface{}, key string, defaultValue int64) int64 {
	if value, exists := options[key]; exists {
		if intVal, ok := value.(int64); ok {
			return intVal
		}
		if intVal, ok := value.(int); ok {
			return int64(intVal)
		}
		if floatVal, ok := value.(float64); ok {
			return int64(floatVal)
		}
	}
	return defaultValue
}

func getBoolOption(options map[string]interface{}, key string, defaultValue bool) bool {
	if value, exists := options[key]; exists {
		if boolVal, ok := value.(bool); ok {
			return boolVal
		}
	}
	return defaultValue
}

func getDurationOption(options map[string]interface{}, key string, defaultValue time.Duration) time.Duration {
	if value, exists := options[key]; exists {
		if str, ok := value.(string); ok {
			if duration, err := time.ParseDuration(str); err == nil {
				return duration
			}
		}
	}
	return defaultValue
}

func getMapStringOption(options map[string]interface{}, key string) map[string]string {
	result := make(map[string]string)
	if value, exists := options[key]; exists {
		if mapVal, ok := value.(map[string]interface{}); ok {
			for k, v := range mapVal {
				if str, ok := v.(string); ok {
					result[k] = str
				}
			}
		}
	}
	return result
}
