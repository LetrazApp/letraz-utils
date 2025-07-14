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
	"letraz-utils/internal/logging"
	"letraz-utils/internal/mux"
	"letraz-utils/internal/scraper/workers"

	"github.com/labstack/echo/v4"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize the new logging system
	if err := logging.InitializeLogging(cfg); err != nil {
		log.Fatalf("Failed to initialize logging: %v", err)
	}
	defer logging.CloseLogging()

	// Get the new logger instance
	logger := logging.GetGlobalLogger()
	logger.Info("Starting Letraz Job Scraper with new logging system")

	// Log a test message to verify Betterstack integration
	logger.Info("Testing Betterstack integration", map[string]interface{}{
		"environment":         os.Getenv("GO_ENV"),
		"betterstack_enabled": os.Getenv("BETTERSTACK_ENABLED"),
		"timestamp":           time.Now().Unix(),
	})

	// Initialize LLM manager
	llmManager := llm.NewManager(cfg)
	if err := llmManager.Start(); err != nil {
		logger.Error("Failed to start LLM manager", map[string]interface{}{"error": err.Error()})
		return
	}

	// Initialize background task manager
	logger.Info("Initializing background task manager")
	taskManager := background.NewTaskManager(cfg)
	ctx := context.Background()
	if err := taskManager.Start(ctx); err != nil {
		logger.Error("Failed to start task manager", map[string]interface{}{"error": err.Error()})
		return
	}

	// Initialize worker pool
	logger.Debug("DEBUG: About to initialize worker pool")
	poolManager := workers.NewPoolManager(cfg, llmManager)
	logger.Debug("DEBUG: PoolManager created")

	if err := poolManager.Initialize(); err != nil {
		logger.Error("Failed to start worker pool", map[string]interface{}{"error": err.Error()})
		return
	}
	logger.Debug("DEBUG: PoolManager initialized successfully")

	defer func() {
		if err := poolManager.Shutdown(); err != nil {
			logger.Error("Error shutting down pool manager", map[string]interface{}{"error": err.Error()})
		}
	}()

	// Initialize Echo
	e := echo.New()

	// Setup routes
	routes.SetupRoutes(e, cfg, poolManager, llmManager, taskManager)

	// Initialize multiplexer (gRPC + HTTP)
	multiplexer := mux.NewMultiplexer(cfg, poolManager, llmManager, taskManager, e)

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logger.Info("Shutting down server...")

		// Create a shutdown context with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Stop multiplexer first (includes both gRPC and HTTP servers)
		logger.Info("Stopping multiplexer...")
		if err := multiplexer.Stop(); err != nil {
			logger.Error("Error stopping multiplexer", map[string]interface{}{"error": err.Error()})
		}

		// Stop task manager
		logger.Info("Stopping background task manager...")
		if err := taskManager.Stop(shutdownCtx); err != nil {
			logger.Error("Error stopping task manager", map[string]interface{}{"error": err.Error()})
		}

		// Stop worker pool
		logger.Info("Stopping worker pool...")
		if err := poolManager.Shutdown(); err != nil {
			logger.Error("Error stopping worker pool", map[string]interface{}{"error": err.Error()})
		}

		// Stop LLM manager
		logger.Info("Stopping LLM manager...")
		if err := llmManager.Stop(); err != nil {
			logger.Error("Error stopping LLM manager", map[string]interface{}{"error": err.Error()})
		}

		logger.Info("Server shutdown complete")
	}()

	// Start server with multiplexer (gRPC + HTTP on same port)
	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Info("Starting multiplexer (gRPC + HTTP)", map[string]interface{}{"address": address})

	if err := multiplexer.Start(address); err != nil {
		logger.Error("Multiplexer failed to start", map[string]interface{}{"error": err.Error()})
		return
	}

	// Wait for the multiplexer to finish
	multiplexer.Wait()
}
