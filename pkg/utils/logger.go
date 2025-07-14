package utils

// This file previously contained legacy logging functions that have been migrated
// to the new logging system in internal/logging/.
//
// The old functions (GetLogger, InitLogger, LogWithRequestID) have been removed
// as they are no longer used anywhere in the codebase.
//
// All logging now uses the centralized logging system with support for:
// - Multiple adapters (console, file, betterstack)
// - Structured logging with consistent format
// - Environment-based configuration
//
// For logging functionality, use: letraz-utils/internal/logging
