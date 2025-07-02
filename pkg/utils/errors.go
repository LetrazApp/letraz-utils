package utils

import (
	"fmt"
	"net/http"
)

// CustomError represents a custom application error
type CustomError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *CustomError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	return e.Message
}

// Common error constructors
func NewBadRequestError(message string) *CustomError {
	return &CustomError{
		Code:    http.StatusBadRequest,
		Message: message,
	}
}

func NewInternalServerError(message string) *CustomError {
	return &CustomError{
		Code:    http.StatusInternalServerError,
		Message: message,
	}
}

func NewTimeoutError(message string) *CustomError {
	return &CustomError{
		Code:    http.StatusRequestTimeout,
		Message: message,
	}
}

func NewValidationError(detail string) *CustomError {
	return &CustomError{
		Code:    http.StatusBadRequest,
		Message: "Validation failed",
		Detail:  detail,
	}
}

// Scraping specific errors
func NewScrapingError(detail string) *CustomError {
	return &CustomError{
		Code:    http.StatusUnprocessableEntity,
		Message: "Scraping failed",
		Detail:  detail,
	}
}

func NewLLMError(detail string) *CustomError {
	return &CustomError{
		Code:    http.StatusBadGateway,
		Message: "LLM processing failed",
		Detail:  detail,
	}
}

// NewNotJobPostingError returns an error when the URL doesn't contain a job posting
func NewNotJobPostingError(detail string) *CustomError {
	return &CustomError{
		Code:    http.StatusUnprocessableEntity,
		Message: "Content is not a job posting",
		Detail:  detail,
	}
}
