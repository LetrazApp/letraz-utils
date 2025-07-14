package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"letraz-utils/internal/logging"
)

// ProtoHandler serves the protobuf definition file
func ProtoHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		logger := logging.GetGlobalLogger()

		// Get the proto file path
		protoPath := filepath.Join("api", "proto", "letraz", "v1", "letraz-utils.proto")

		// Check if file exists
		if _, err := os.Stat(protoPath); os.IsNotExist(err) {
			logger.Error("Proto file not found", map[string]interface{}{"error": err})
			return c.JSON(http.StatusNotFound, map[string]string{
				"error": "Proto file not found",
			})
		}

		// Read the proto file
		content, err := os.ReadFile(protoPath)
		if err != nil {
			logger.Error("Failed to read proto file", map[string]interface{}{"error": err})
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to read proto file",
			})
		}

		// Set appropriate headers
		c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Response().Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour
		c.Response().Header().Set("ETag", `"letraz-utils-v1"`)
		c.Response().Header().Set("X-Proto-Version", "v1")
		c.Response().Header().Set("X-Service-Name", "letraz-utils")

		// Handle If-None-Match for caching
		if match := c.Request().Header.Get("If-None-Match"); match == `"letraz-utils-v1"` {
			return c.NoContent(http.StatusNotModified)
		}

		logger.Info("Proto file served", map[string]interface{}{
			"client_ip":  c.RealIP(),
			"user_agent": c.Request().UserAgent(),
		})

		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", content)
	}
}

// ProtoMetadataHandler provides metadata about the proto file
func ProtoMetadataHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		// Get file info
		protoPath := filepath.Join("api", "proto", "letraz", "v1", "letraz-utils.proto")
		fileInfo, err := os.Stat(protoPath)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{
				"error": "Proto file not found",
			})
		}

		metadata := map[string]interface{}{
			"service_name":  "letraz-utils",
			"proto_version": "v1",
			"file_size":     fileInfo.Size(),
			"last_modified": fileInfo.ModTime().Format(time.RFC3339),
			"download_url":  "/api/v1/proto/letraz-utils.proto",
			"grpc_services": []string{
				"letraz.v1.ScraperService",
				"letraz.v1.ResumeService",
			},
			"supported_features": []string{
				"async_processing",
				"multiplexed_protocols",
				"structured_logging",
			},
		}

		return c.JSON(http.StatusOK, metadata)
	}
}
