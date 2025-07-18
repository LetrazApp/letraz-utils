package mux

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/soheilhy/cmux"
	"letraz-utils/internal/background"
	"letraz-utils/internal/config"
	"letraz-utils/internal/grpc/server"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/scraper/workers"
)

// Multiplexer handles protocol detection and routing between gRPC and HTTP
type Multiplexer struct {
	cfg         *config.Config
	poolManager *workers.PoolManager
	llmManager  *llm.Manager
	taskManager background.TaskManager
	logger      logging.Logger

	// Servers
	grpcServer *server.Server
	httpServer *http.Server

	// Multiplexer
	mux      cmux.CMux
	listener net.Listener

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMultiplexer creates a new protocol multiplexer
func NewMultiplexer(cfg *config.Config, poolManager *workers.PoolManager, llmManager *llm.Manager, taskManager background.TaskManager, httpHandler http.Handler) *Multiplexer {
	ctx, cancel := context.WithCancel(context.Background())

	return &Multiplexer{
		cfg:         cfg,
		poolManager: poolManager,
		llmManager:  llmManager,
		taskManager: taskManager,
		logger:      logging.GetGlobalLogger(),
		ctx:         ctx,
		cancel:      cancel,
		httpServer: &http.Server{
			Handler:           httpHandler,
			ReadTimeout:       cfg.Server.ReadTimeout,
			WriteTimeout:      cfg.Server.WriteTimeout,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       cfg.Server.IdleTimeout,
		},
	}
}

// Start starts the multiplexer and both servers
func (m *Multiplexer) Start(address string) error {
	// Create the main listener
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	m.listener = listener

	// Create the multiplexer
	m.mux = cmux.New(listener)

	// Create matchers for different protocols
	grpcListener := m.mux.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
	httpListener := m.mux.Match(cmux.HTTP1Fast())

	// Create gRPC server
	m.grpcServer = server.NewServer(m.cfg, m.poolManager, m.llmManager, m.taskManager)

	// Start gRPC server
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.logger.Info("Starting gRPC server", map[string]interface{}{
			"address": address,
		})
		if err := m.grpcServer.Start(grpcListener); err != nil {
			m.logger.Error("gRPC server failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Start HTTP server
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.logger.Info("Starting HTTP server", map[string]interface{}{
			"address": address,
		})
		if err := m.httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			m.logger.Error("HTTP server failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Start the multiplexer
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.logger.Info("Starting protocol multiplexer", map[string]interface{}{
			"address": address,
		})
		if err := m.mux.Serve(); err != nil {
			m.logger.Error("Multiplexer failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	m.logger.Info("Multiplexer started successfully", map[string]interface{}{
		"address": address,
	})
	return nil
}

// Stop gracefully shuts down the multiplexer and both servers
func (m *Multiplexer) Stop() error {
	m.logger.Info("Stopping multiplexer...", nil)

	// Cancel context to signal shutdown
	m.cancel()

	// Create a timeout context for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop HTTP server gracefully
	if err := m.httpServer.Shutdown(shutdownCtx); err != nil {
		m.logger.Error("HTTP server shutdown failed", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Stop gRPC server
	if m.grpcServer != nil {
		m.grpcServer.Stop()
	}

	// Close the main listener
	if m.listener != nil {
		if err := m.listener.Close(); err != nil {
			m.logger.Error("Failed to close listener", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Wait for all goroutines to finish or timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("Multiplexer stopped gracefully", nil)
	case <-shutdownCtx.Done():
		m.logger.Warn("Multiplexer shutdown timed out", nil)
	}

	return nil
}

// Wait waits for the multiplexer to finish
func (m *Multiplexer) Wait() {
	m.wg.Wait()
}

// IsHealthy checks if the multiplexer and both servers are healthy
func (m *Multiplexer) IsHealthy() bool {
	// Check if context is still valid
	if m.ctx.Err() != nil {
		return false
	}

	// Check if listener is still valid
	if m.listener == nil {
		return false
	}

	// Basic health check - could be extended with more sophisticated checks
	return true
}

// GetGRPCServer returns the gRPC server instance
func (m *Multiplexer) GetGRPCServer() *server.Server {
	return m.grpcServer
}

// GetHTTPServer returns the HTTP server instance
func (m *Multiplexer) GetHTTPServer() *http.Server {
	return m.httpServer
}

// GetListener returns the main listener
func (m *Multiplexer) GetListener() net.Listener {
	return m.listener
}

// GetAddress returns the address the multiplexer is listening on
func (m *Multiplexer) GetAddress() string {
	if m.listener != nil {
		return m.listener.Addr().String()
	}
	return ""
}
