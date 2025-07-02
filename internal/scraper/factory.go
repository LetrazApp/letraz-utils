package scraper

import (
	"fmt"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/llm"
	"letraz-scrapper/internal/scraper/engines/firecrawl"
	"letraz-scrapper/internal/scraper/engines/headed"
	"letraz-scrapper/internal/scraper/engines/hybrid"
)

// DefaultScraperFactory implements ScraperFactory
type DefaultScraperFactory struct {
	config     *config.Config
	llmManager *llm.Manager
}

// NewScraperFactory creates a new scraper factory
func NewScraperFactory(cfg *config.Config, llmManager *llm.Manager) ScraperFactory {
	return &DefaultScraperFactory{
		config:     cfg,
		llmManager: llmManager,
	}
}

// CreateScraper creates a new scraper instance for the given engine
func (f *DefaultScraperFactory) CreateScraper(engine string) (Scraper, error) {
	switch engine {
	case "hybrid":
		return hybrid.NewHybridScraper(f.config, f.llmManager), nil
	case "firecrawl":
		return firecrawl.NewFirecrawlScraper(f.config, f.llmManager), nil
	case "headed", "rod":
		return headed.NewRodScraper(f.config, f.llmManager), nil
	case "auto":
		// Auto mode defaults to hybrid for best performance and fallback capability
		return hybrid.NewHybridScraper(f.config, f.llmManager), nil
	default:
		return nil, fmt.Errorf("unsupported scraper engine: %s", engine)
	}
}

// GetSupportedEngines returns a list of supported engine types
func (f *DefaultScraperFactory) GetSupportedEngines() []string {
	return []string{"firecrawl", "headed", "auto"}
}
