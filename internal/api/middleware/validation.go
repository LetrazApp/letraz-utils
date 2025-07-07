package middleware

import (
	"net/http"
	"time"

	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"

	"github.com/labstack/echo/v4"
)

// RequestValidation middleware validates incoming requests
func RequestValidation() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Add request ID to context
			requestID := utils.GenerateRequestID()
			c.Set("request_id", requestID)
			c.Response().Header().Set("X-Request-ID", requestID)

			// Content length validation for POST requests
			if c.Request().Method == http.MethodPost {
				contentLength := c.Request().ContentLength
				if contentLength > 1024*1024 { // 1MB limit
					return c.JSON(http.StatusRequestEntityTooLarge, models.ErrorResponse{
						Error:     "request_too_large",
						Message:   "Request body too large",
						RequestID: requestID,
						Timestamp: time.Now(),
					})
				}
			}

			return next(c)
		}
	}
}
