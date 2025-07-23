package llm

import (
	"context"
	"fmt"
	"sync"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/pkg/models"
)

// Manager manages LLM providers and their lifecycle
type Manager struct {
	config   *config.Config
	factory  *LLMFactory
	provider LLMProvider
	logger   types.Logger
	mu       sync.RWMutex
	healthy  bool
}

// NewManager creates a new LLM manager instance
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config:  cfg,
		factory: NewLLMFactory(cfg),
		logger:  logging.GetGlobalLogger(),
	}
}

// Start initializes the LLM manager and creates the provider
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Starting LLM manager", map[string]interface{}{
		"provider": m.config.LLM.Provider,
	})

	// Create provider
	provider, err := m.factory.CreateProvider()
	if err != nil {
		return fmt.Errorf("failed to create LLM provider: %w", err)
	}

	m.provider = provider

	// Test provider health
	ctx, cancel := context.WithTimeout(context.Background(), m.config.LLM.Timeout)
	defer cancel()

	if err := m.provider.IsHealthy(ctx); err != nil {
		m.logger.Warn("LLM provider health check failed - LLM features will be disabled", map[string]interface{}{
			"error": err.Error(),
		})
		m.healthy = false
		// Don't return error - allow server to start without LLM
	} else {
		m.healthy = true
		m.logger.Info("LLM manager started successfully", map[string]interface{}{
			"provider": m.provider.GetProviderName(),
		})
	}

	return nil
}

// Stop shuts down the LLM manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Stopping LLM manager", map[string]interface{}{})
	m.provider = nil
	m.healthy = false
	return nil
}

// ExtractJobData extracts job data from HTML using the configured LLM provider
func (m *Manager) ExtractJobData(ctx context.Context, html, url string) (*models.Job, error) {
	m.mu.RLock()
	provider := m.provider
	healthy := m.healthy
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("LLM manager not started or provider not available")
	}

	if !healthy {
		return nil, fmt.Errorf("LLM provider is not available - check API key configuration (set LLM_API_KEY environment variable)")
	}

	return provider.ExtractJobData(ctx, html, url)
}

// ExtractJobFromDescription extracts job data from description text using the configured LLM provider
func (m *Manager) ExtractJobFromDescription(ctx context.Context, description string) (*models.Job, error) {
	m.mu.RLock()
	provider := m.provider
	healthy := m.healthy
	m.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("LLM manager not started or provider not available")
	}

	if !healthy {
		return nil, fmt.Errorf("LLM provider is not available - check API key configuration (set LLM_API_KEY environment variable)")
	}

	return provider.ExtractJobFromDescription(ctx, description)
}

// TailorResume tailors a resume for a specific job using the configured LLM provider
func (m *Manager) TailorResume(ctx context.Context, baseResume *models.BaseResume, job *models.Job) (*models.TailoredResume, []models.Suggestion, error) {
	m.mu.RLock()
	provider := m.provider
	healthy := m.healthy
	m.mu.RUnlock()

	if provider == nil {
		return nil, nil, fmt.Errorf("LLM manager not started or provider not available")
	}

	if !healthy {
		return nil, nil, fmt.Errorf("LLM provider is not available - check API key configuration (set LLM_API_KEY environment variable)")
	}

	return provider.TailorResume(ctx, baseResume, job)
}

// TailorResumeWithRawResponse tailors a resume and returns the raw AI response for conversation history
func (m *Manager) TailorResumeWithRawResponse(ctx context.Context, baseResume *models.BaseResume, job *models.Job) (*models.TailoredResume, []models.Suggestion, string, error) {
	m.mu.RLock()
	provider := m.provider
	healthy := m.healthy
	m.mu.RUnlock()

	if provider == nil {
		return nil, nil, "", fmt.Errorf("LLM manager not started or provider not available")
	}

	if !healthy {
		return nil, nil, "", fmt.Errorf("LLM provider is not available - check API key configuration (set LLM_API_KEY environment variable)")
	}

	return provider.TailorResumeWithRawResponse(ctx, baseResume, job)
}

// IsHealthy checks if the LLM manager and provider are healthy
func (m *Manager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthy && m.provider != nil
}

// GetProviderName returns the name of the current LLM provider
func (m *Manager) GetProviderName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.provider != nil {
		return m.provider.GetProviderName()
	}
	return "none"
}

// CheckHealth performs a health check on the LLM provider
func (m *Manager) CheckHealth(ctx context.Context) error {
	m.mu.RLock()
	provider := m.provider
	m.mu.RUnlock()

	if provider == nil {
		return fmt.Errorf("LLM provider not available")
	}

	err := provider.IsHealthy(ctx)

	m.mu.Lock()
	m.healthy = (err == nil)
	m.mu.Unlock()

	return err
}
