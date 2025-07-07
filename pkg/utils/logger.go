package utils

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Logger *logrus.Logger

// InitLogger initializes the global logger with the specified configuration
func InitLogger(level, format string) {
	Logger = logrus.New()

	// Set log level
	switch level {
	case "debug":
		Logger.SetLevel(logrus.DebugLevel)
	case "info":
		Logger.SetLevel(logrus.InfoLevel)
	case "warn":
		Logger.SetLevel(logrus.WarnLevel)
	case "error":
		Logger.SetLevel(logrus.ErrorLevel)
	default:
		Logger.SetLevel(logrus.InfoLevel)
	}

	// Set output format
	if format == "json" {
		Logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		Logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	Logger.SetOutput(os.Stdout)
}

// GetLogger returns the global logger instance
func GetLogger() *logrus.Logger {
	if Logger == nil {
		InitLogger("info", "json")
	}
	return Logger
}

// LogWithRequestID creates a logger with request ID context
func LogWithRequestID(requestID string) *logrus.Entry {
	return GetLogger().WithField("request_id", requestID)
}
