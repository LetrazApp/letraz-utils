package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"letraz-scrapper/internal/api/routes"
	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/scraper/workers"
	"letraz-scrapper/pkg/utils"

	"github.com/labstack/echo/v4"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	utils.InitLogger(cfg.Logging.Level, cfg.Logging.Format)
	logger := utils.GetLogger()

	logger.Info("Starting Letraz Job Scraper service")

	// Initialize worker pool manager
	poolManager := workers.NewPoolManager(cfg)
	err = poolManager.Initialize()
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize worker pool")
	}
	logger.Info("Worker pool initialized successfully")

	// Create Echo instance
	e := echo.New()

	// Disable Echo's banner
	e.HideBanner = true

	// Setup routes with worker pool manager
	routes.SetupRoutes(e, cfg, poolManager)

	// Start server in a goroutine
	go func() {
		address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		logger.WithField("address", address).Info("Starting HTTP server")

		// Configure server timeouts
		s := &http.Server{
			Addr:         address,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		}

		if err := e.StartServer(s); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown worker pool first
	logger.Info("Shutting down worker pool...")
	if err := poolManager.Shutdown(); err != nil {
		logger.WithError(err).Error("Error shutting down worker pool")
	}

	// Shutdown HTTP server
	if err := e.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
		os.Exit(1)
	}

	logger.Info("Server shutdown complete")
}
