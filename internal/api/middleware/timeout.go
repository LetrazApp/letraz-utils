package middleware

import (
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// TimeoutConfig returns timeout middleware configuration
func TimeoutConfig(timeout time.Duration) echo.MiddlewareFunc {
	return middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: timeout,
	})
}

// SelectiveTimeoutConfig returns selective timeout middleware that applies different timeouts based on route
func SelectiveTimeoutConfig(defaultTimeout time.Duration, longTimeout time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path

			// Apply longer timeout for AI-intensive endpoints
			if strings.Contains(path, "/resume/tailor") {
				timeoutMiddleware := middleware.TimeoutWithConfig(middleware.TimeoutConfig{
					Timeout: longTimeout,
				})
				return timeoutMiddleware(next)(c)
			}

			// Apply default timeout for other endpoints
			timeoutMiddleware := middleware.TimeoutWithConfig(middleware.TimeoutConfig{
				Timeout: defaultTimeout,
			})
			return timeoutMiddleware(next)(c)
		}
	}
}
