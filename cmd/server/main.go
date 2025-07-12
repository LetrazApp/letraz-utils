package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"letraz-utils/internal/api/routes"
	"letraz-utils/internal/background"
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

	// Initialize background task manager
	logger.Info("Initializing background task manager")
	taskManager := background.NewTaskManager(cfg)
	ctx := context.Background()
	if err := taskManager.Start(ctx); err != nil {
		logger.WithError(err).Fatal("Failed to start task manager")
	}

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
	routes.SetupRoutes(e, cfg, poolManager, llmManager, taskManager)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logger.Info("Shutting down server...")

		// Create a shutdown context with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Stop task manager first (most important for background tasks)
		logger.Info("Stopping background task manager...")
		if err := taskManager.Stop(shutdownCtx); err != nil {
			logger.WithError(err).Error("Error stopping task manager")
		}

		// Stop worker pool
		logger.Info("Stopping worker pool...")
		if err := poolManager.Shutdown(); err != nil {
			logger.WithError(err).Error("Error stopping worker pool")
		}

		// Stop LLM manager
		logger.Info("Stopping LLM manager...")
		if err := llmManager.Stop(); err != nil {
			logger.WithError(err).Error("Error stopping LLM manager")
		}

		// Shutdown Echo server
		logger.Info("Stopping HTTP server...")
		if err := e.Shutdown(shutdownCtx); err != nil {
			logger.WithError(err).Error("Error shutting down server")
		}

		logger.Info("Server shutdown complete")
	}()

	// Start server
	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.WithField("address", address).Info("Server starting")

	if err := e.Start(address); err != nil {
		logger.WithError(err).Fatal("Server failed to start")
	}
}
