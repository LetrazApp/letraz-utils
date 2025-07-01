package llm

import (
	"fmt"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/llm/providers"
)

// LLMFactory creates LLM provider instances
type LLMFactory struct {
	config *config.Config
}

// NewLLMFactory creates a new LLM factory instance
func NewLLMFactory(cfg *config.Config) *LLMFactory {
	return &LLMFactory{
		config: cfg,
	}
}

// CreateProvider creates an LLM provider based on the configuration
func (f *LLMFactory) CreateProvider() (LLMProvider, error) {
	switch f.config.LLM.Provider {
	case "claude":
		return providers.NewClaudeProvider(f.config), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", f.config.LLM.Provider)
	}
}

// GetSupportedProviders returns a list of supported LLM providers
func (f *LLMFactory) GetSupportedProviders() []string {
	return []string{"claude"}
}
