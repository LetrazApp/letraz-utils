package logging

import (
	"fmt"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging/adapters"
)

// Manager manages the logging system initialization and configuration
type Manager struct {
	factory *AdapterFactory
	logger  *MultiLogger
}

// NewManager creates a new logging manager
func NewManager() *Manager {
	return &Manager{
		factory: NewAdapterFactory(),
		logger:  NewMultiLogger(),
	}
}

// Initialize initializes the logging system from configuration
func (m *Manager) Initialize(cfg *config.Config) error {
	// Set the log level
	level := ParseLogLevel(cfg.Logging.Level)
	m.logger.SetLevel(level)

	// If new adapter configuration is provided, use it
	if len(cfg.Logging.Adapters) > 0 {
		return m.initializeFromAdapters(cfg.Logging.Adapters)
	}

	// Fallback to legacy configuration
	return m.initializeFromLegacyConfig(cfg)
}

// initializeFromAdapters initializes logging adapters from the new configuration format
func (m *Manager) initializeFromAdapters(adapterConfigs []struct {
	Name    string                 `yaml:"name"`
	Type    string                 `yaml:"type"`
	Enabled bool                   `yaml:"enabled"`
	Options map[string]interface{} `yaml:"options"`
}) error {
	for _, adapterConfig := range adapterConfigs {
		if !adapterConfig.Enabled {
			continue
		}

		// Convert to our internal adapter config
		config := AdapterConfig{
			Name:    adapterConfig.Name,
			Type:    adapterConfig.Type,
			Enabled: adapterConfig.Enabled,
			Options: adapterConfig.Options,
		}

		adapter, err := m.factory.CreateAdapter(config)
		if err != nil {
			return fmt.Errorf("failed to create adapter %s: %w", adapterConfig.Name, err)
		}

		if err := m.logger.AddAdapter(adapter); err != nil {
			return fmt.Errorf("failed to add adapter %s: %w", adapterConfig.Name, err)
		}
	}

	return nil
}

// initializeFromLegacyConfig initializes logging from legacy configuration for backward compatibility
func (m *Manager) initializeFromLegacyConfig(cfg *config.Config) error {
	// Create a stdout adapter based on legacy config
	stdoutConfig := adapters.StdoutConfig{
		Format:    cfg.Logging.Format,
		Colorized: false, // Legacy config doesn't support colorization
	}

	adapter := adapters.NewStdoutAdapter("legacy_stdout", stdoutConfig)
	if err := m.logger.AddAdapter(adapter); err != nil {
		return fmt.Errorf("failed to add legacy stdout adapter: %w", err)
	}

	return nil
}

// GetLogger returns the initialized logger
func (m *Manager) GetLogger() Logger {
	return m.logger
}

// Close closes the logging system
func (m *Manager) Close() error {
	if m.logger != nil {
		return m.logger.Close()
	}
	return nil
}

// Global manager instance
var globalManager *Manager

// InitializeLogging initializes the global logging system
func InitializeLogging(cfg *config.Config) error {
	globalManager = NewManager()
	return globalManager.Initialize(cfg)
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() Logger {
	if globalManager == nil {
		// Fallback to a basic logger if not initialized
		manager := NewManager()
		stdoutConfig := adapters.StdoutConfig{
			Format:    "json",
			Colorized: false,
		}
		adapter := adapters.NewStdoutAdapter("fallback_stdout", stdoutConfig)
		manager.logger.AddAdapter(adapter)
		globalManager = manager
	}
	return globalManager.GetLogger()
}

// CloseLogging closes the global logging system
func CloseLogging() error {
	if globalManager != nil {
		return globalManager.Close()
	}
	return nil
}

// LogWithRequestID creates a logger with request ID context (compatibility function)
func LogWithRequestID(requestID string) Logger {
	return GetGlobalLogger().WithField("request_id", requestID)
}

// Legacy compatibility functions to maintain backward compatibility
func Debug(message string, fields ...map[string]interface{}) {
	GetGlobalLogger().Debug(message, fields...)
}

func Info(message string, fields ...map[string]interface{}) {
	GetGlobalLogger().Info(message, fields...)
}

func Warn(message string, fields ...map[string]interface{}) {
	GetGlobalLogger().Warn(message, fields...)
}

func Error(message string, fields ...map[string]interface{}) {
	GetGlobalLogger().Error(message, fields...)
}

func Fatal(message string, fields ...map[string]interface{}) {
	GetGlobalLogger().Fatal(message, fields...)
}
