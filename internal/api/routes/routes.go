package routes

import (
	"letraz-utils/internal/api/handlers"
	"letraz-utils/internal/api/middleware"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/scraper/workers"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
)

// SetupRoutes configures all API routes
func SetupRoutes(e *echo.Echo, cfg *config.Config, poolManager *workers.PoolManager, llmManager *llm.Manager, taskManager background.TaskManager) {
	// Global middleware
	e.Use(echomiddleware.Logger())
	e.Use(echomiddleware.Recover())
	e.Use(middleware.CORSConfig())
	e.Use(middleware.RequestValidation())
	// Use selective timeout: 30s for most endpoints, 2 minutes for AI-intensive endpoints
	e.Use(middleware.SelectiveTimeoutConfig(cfg.Server.ReadTimeout, 2*time.Minute))

	// Health check routes
	health := e.Group("/health")
	{
		health.GET("", handlers.HealthHandler)
		health.GET("/ready", handlers.ReadinessHandler)
		health.GET("/live", handlers.LivenessHandler)
		health.GET("/workers", handlers.WorkerHealthHandler(poolManager))
	}

	// Status route
	e.GET("/status", handlers.StatusHandler)

	// API v1 routes
	v1 := e.Group("/api/v1")
	{
		v1.POST("/scrape", handlers.ScrapeHandler(cfg, poolManager, taskManager))

		// Resume tailoring routes
		resume := v1.Group("/resume")
		{
			resume.POST("/tailor", handlers.TailorResumeHandler(cfg, llmManager, taskManager))
		}

		// Proto file serving routes
		proto := v1.Group("/proto")
		{
			proto.GET("/letraz-utils.proto", handlers.ProtoHandler())
			proto.GET("/letraz-utils/metadata", handlers.ProtoMetadataHandler())
		}

		// Worker monitoring routes
		workers := v1.Group("/workers")
		{
			workers.GET("/stats", handlers.WorkerStatsHandler(poolManager))
			workers.GET("/status", handlers.DetailedWorkerStatusHandler(poolManager))
		}

		// Domain-specific routes
		domains := v1.Group("/domains")
		{
			domains.GET("/:domain/stats", handlers.DomainStatsHandler(poolManager))
		}
	}

	// Root route
	e.GET("/", func(c echo.Context) error {
		return c.JSON(200, map[string]string{
			"service": "Letraz Job Scraper",
			"version": "1.0.0",
			"status":  "running",
		})
	})
}
