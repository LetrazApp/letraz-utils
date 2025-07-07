package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"letraz-utils/internal/api/routes"
	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/scraper/workers"
	"letraz-utils/pkg/utils"

	"github.com/labstack/echo/v4"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger := utils.GetLogger()
	logger.Info("Starting Letraz Job Scraper")

	// Initialize LLM manager
	llmManager := llm.NewManager(cfg)
	if err := llmManager.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start LLM manager")
	}
	defer llmManager.Stop()

	// Initialize worker pool
	logger.Debug("DEBUG: About to initialize worker pool")
	poolManager := workers.NewPoolManager(cfg, llmManager)
	logger.Debug("DEBUG: PoolManager created")

	if err := poolManager.Initialize(); err != nil {
		logger.WithError(err).Fatal("Failed to start worker pool")
	}
	logger.Debug("DEBUG: PoolManager initialized successfully")

	defer poolManager.Shutdown()

	// Initialize Echo
	e := echo.New()

	// Setup routes
	routes.SetupRoutes(e, cfg, poolManager)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logger.Info("Shutting down server...")

		// Stop worker pool
		if err := poolManager.Shutdown(); err != nil {
			logger.WithError(err).Error("Error stopping worker pool")
		}

		// Stop LLM manager
		if err := llmManager.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping LLM manager")
		}

		// Shutdown Echo
		if err := e.Shutdown(nil); err != nil {
			logger.WithError(err).Error("Error shutting down server")
		}
	}()

	// Start server
	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.WithField("address", address).Info("Server starting")

	if err := e.Start(address); err != nil {
		logger.WithError(err).Fatal("Server failed to start")
	}
}
