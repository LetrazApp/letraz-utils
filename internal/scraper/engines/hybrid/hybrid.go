package hybrid

import (
	"context"
	"fmt"
	"net/http"

	"letraz-utils/internal/config"
	"letraz-utils/internal/llm"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
	"letraz-utils/internal/scraper/engines/firecrawl"
	"letraz-utils/internal/scraper/engines/headed"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// HybridScraper implements a hybrid approach: try Rod scraper first, fallback to Firecrawl if a captcha is detected
type HybridScraper struct {
	config           *config.Config
	llmManager       *llm.Manager
	rodScraper       *headed.RodScraper
	firecrawlScraper *firecrawl.FirecrawlScraper
	captchaDomainMgr *utils.CaptchaDomainManager
	logger           types.Logger
	usedRod          bool // Track if Rod scraper was actually used
	usedFirecrawl    bool // Track if Firecrawl scraper was actually used
}

// NewHybridScraper creates a new hybrid scraper instance
func NewHybridScraper(cfg *config.Config, llmManager *llm.Manager) *HybridScraper {
	logger := logging.GetGlobalLogger()

	// Initialize both scrapers
	rodScraper := headed.NewRodScraper(cfg, llmManager)
	firecrawlScraper := firecrawl.NewFirecrawlScraper(cfg, llmManager)

	if rodScraper == nil {
		logger.Error("Failed to initialize Rod scraper for hybrid engine", map[string]interface{}{})
		return nil
	}

	if firecrawlScraper == nil {
		logger.Error("Failed to initialize Firecrawl scraper for hybrid engine", map[string]interface{}{})
		return nil
	}

	// Initialize captcha domain manager
	captchaDomainMgr := utils.NewCaptchaDomainManager()

	logger.Info("Hybrid scraper initialized with Rod (primary) and Firecrawl (fallback)", map[string]interface{}{
		"known_captcha_domains": captchaDomainMgr.GetDomainsCount(),
	})

	return &HybridScraper{
		config:           cfg,
		llmManager:       llmManager,
		rodScraper:       rodScraper,
		firecrawlScraper: firecrawlScraper,
		captchaDomainMgr: captchaDomainMgr,
		logger:           logger,
	}
}

// ScrapeJob scrapes a job posting using hybrid approach: Rod first, Firecrawl on captcha
func (h *HybridScraper) ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error) {
	h.logger.Info("Starting hybrid job scraping (Rod → Firecrawl fallback)", map[string]interface{}{
		"url": url,
	})

	// Reset usage tracking for this job
	h.usedRod = false
	h.usedFirecrawl = false

	// Check if this domain is known to have captcha protection
	if h.captchaDomainMgr.IsKnownCaptchaDomain(url) {
		h.logger.Info("Domain is known to have captcha protection, skipping Rod and using Firecrawl directly", map[string]interface{}{
			"url": url,
		})

		h.logger.Debug("DEBUG: About to call Firecrawl directly", map[string]interface{}{
			"url": url,
		})

		// Mark Firecrawl as used
		h.usedFirecrawl = true

		// Go straight to Firecrawl for known captcha domains
		job, err := h.firecrawlScraper.ScrapeJob(ctx, url, options)

		h.logger.Debug("DEBUG: Firecrawl direct call completed", map[string]interface{}{
			"url":     url,
			"success": err == nil,
		})

		if err != nil {
			h.logger.Debug("DEBUG: Firecrawl direct call failed, returning error", map[string]interface{}{
				"url": url,
			})
			// Don't wrap CustomError types so they can be properly handled upstream
			if _, ok := err.(*utils.CustomError); ok {
				return nil, err
			}
			return nil, fmt.Errorf("firecrawl scraping failed for known captcha domain: %w", err)
		}

		h.logger.Info("Successfully scraped job using Firecrawl (known captcha domain)", map[string]interface{}{
			"url":       url,
			"job_title": job.Title,
			"company":   job.CompanyName,
			"engine":    "firecrawl_direct",
		})

		h.logger.Debug("DEBUG: About to return job result from direct path", map[string]interface{}{
			"url": url,
		})
		return job, nil
	}

	// Try Rod scraper first for unknown domains
	h.logger.Info("Attempting scrape with Rod engine", map[string]interface{}{
		"url": url,
	})

	// Mark Rod as used
	h.usedRod = true

	job, err := h.rodScraper.ScrapeJob(ctx, url, options)

	// Check if it's a captcha error - if so, fallback to Firecrawl
	if err != nil {
		if customErr, ok := err.(*utils.CustomError); ok && customErr.Code == http.StatusTemporaryRedirect {
			h.logger.Info("Rod scraper detected captcha, adding domain to captcha list and falling back to Firecrawl", map[string]interface{}{
				"url":    url,
				"reason": customErr.Detail,
			})

			// Add this domain to the captcha domains list for future optimization
			if addErr := h.captchaDomainMgr.AddCaptchaDomain(url); addErr != nil {
				h.logger.Warn("Failed to add domain to captcha list", map[string]interface{}{
					"url":   url,
					"error": addErr.Error(),
				})
			}

			// Mark Firecrawl as used for fallback
			h.usedFirecrawl = true

			// Fallback to Firecrawl
			h.logger.Info("Attempting scrape with Firecrawl engine", map[string]interface{}{
				"url": url,
			})
			job, err = h.firecrawlScraper.ScrapeJob(ctx, url, options)

			if err != nil {
				h.logger.Error("Firecrawl fallback also failed", map[string]interface{}{
					"url":   url,
					"error": err.Error(),
				})

				// Don't wrap CustomError types so they can be properly handled upstream
				if _, ok := err.(*utils.CustomError); ok {
					return nil, err
				}
				return nil, fmt.Errorf("hybrid scraping failed - Rod: captcha detected, Firecrawl: %w", err)
			}

			h.logger.Info("Successfully scraped job using Firecrawl fallback", map[string]interface{}{
				"url":       url,
				"job_title": job.Title,
				"company":   job.CompanyName,
				"engine":    "firecrawl_fallback",
			})
			return job, nil
		}

		// Non-captcha error from Rod scraper - preserve CustomError types
		h.logger.Error("Rod scraper failed with non-captcha error", map[string]interface{}{
			"url":   url,
			"error": err.Error(),
		})

		// Don't wrap CustomError types so they can be properly handled upstream
		if _, ok := err.(*utils.CustomError); ok {
			return nil, err
		}
		return nil, fmt.Errorf("rod scraper failed: %w", err)
	}

	// Rod scraper succeeded without captcha
	h.logger.Info("Successfully scraped job using Rod engine (no captcha)", map[string]interface{}{
		"url":       url,
		"job_title": job.Title,
		"company":   job.CompanyName,
		"engine":    "rod_primary",
	})
	return job, nil
}

// ScrapeJobLegacy scrapes a job posting using legacy approach
func (h *HybridScraper) ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error) {
	h.logger.Info("Starting hybrid legacy job scraping", map[string]interface{}{
		"url": url,
	})

	// Reset usage tracking for this job
	h.usedRod = false
	h.usedFirecrawl = false

	// For legacy scraping, also check captcha domains but don't add new ones since legacy doesn't detect captcha
	if h.captchaDomainMgr.IsKnownCaptchaDomain(url) {
		h.logger.Info("Domain is known to have captcha protection, using Firecrawl directly for legacy scraping", map[string]interface{}{
			"url": url,
		})

		// Mark Firecrawl as used
		h.usedFirecrawl = true

		jobPosting, err := h.firecrawlScraper.ScrapeJobLegacy(ctx, url, options)
		if err != nil {
			// Don't wrap CustomError types so they can be properly handled upstream
			if _, ok := err.(*utils.CustomError); ok {
				return nil, err
			}
			return nil, fmt.Errorf("firecrawl legacy scraping failed for known captcha domain: %w", err)
		}

		h.logger.Info("Successfully scraped job using Firecrawl legacy (known captcha domain)", map[string]interface{}{
			"url": url,
		})
		return jobPosting, nil
	}

	// Try Rod scraper first for legacy scraping
	h.usedRod = true
	jobPosting, err := h.rodScraper.ScrapeJobLegacy(ctx, url, options)

	// For legacy scraping, we don't expect captcha errors since it doesn't use LLM
	// But if there are issues, we can still fallback to Firecrawl
	if err != nil {
		h.logger.Info("Rod legacy scraper failed, falling back to Firecrawl", map[string]interface{}{
			"url":   url,
			"error": err.Error(),
		})

		// Mark Firecrawl as used for fallback
		h.usedFirecrawl = true

		// Fallback to Firecrawl legacy
		jobPosting, err = h.firecrawlScraper.ScrapeJobLegacy(ctx, url, options)
		if err != nil {
			// Don't wrap CustomError types so they can be properly handled upstream
			if _, ok := err.(*utils.CustomError); ok {
				return nil, err
			}
			return nil, fmt.Errorf("hybrid legacy scraping failed - both Rod and Firecrawl failed: %w", err)
		}

		h.logger.Info("Successfully scraped job using Firecrawl legacy fallback", map[string]interface{}{
			"url": url,
		})
	} else {
		h.logger.Info("Successfully scraped job using Rod legacy", map[string]interface{}{
			"url": url,
		})
	}

	return jobPosting, nil
}

// Cleanup releases any resources used by scrapers that were actually used
func (h *HybridScraper) Cleanup() {
	h.logger.Info("Cleaning up hybrid scraper resources", map[string]interface{}{
		"used_rod":       h.usedRod,
		"used_firecrawl": h.usedFirecrawl,
	})

	// Only cleanup Rod if it was actually used
	if h.usedRod && h.rodScraper != nil {
		h.logger.Info("Cleaning up Rod scraper (was used)")
		h.rodScraper.Cleanup()
	} else {
		h.logger.Info("Skipping Rod scraper cleanup (not used)")
	}

	// Only cleanup Firecrawl if it was actually used
	if h.usedFirecrawl && h.firecrawlScraper != nil {
		h.logger.Info("Cleaning up Firecrawl scraper (was used)")
		h.firecrawlScraper.Cleanup()
	} else {
		h.logger.Info("Skipping Firecrawl scraper cleanup (not used)")
	}
}

// IsHealthy checks if both scrapers are healthy
func (h *HybridScraper) IsHealthy() bool {
	rodHealthy := h.rodScraper != nil && h.rodScraper.IsHealthy()
	firecrawlHealthy := h.firecrawlScraper != nil && h.firecrawlScraper.IsHealthy()

	h.logger.Debug("Hybrid scraper health check", map[string]interface{}{
		"rod_healthy":           rodHealthy,
		"firecrawl_healthy":     firecrawlHealthy,
		"known_captcha_domains": h.captchaDomainMgr.GetDomainsCount(),
	})

	// As long as at least one scraper is healthy, we consider the hybrid healthy
	// Firecrawl is more critical since it's our fallback
	return firecrawlHealthy && rodHealthy
}

// GetCaptchaDomains returns information about known captcha domains (for debugging/monitoring)
func (h *HybridScraper) GetCaptchaDomains() map[string]interface{} {
	return map[string]interface{}{
		"count":   h.captchaDomainMgr.GetDomainsCount(),
		"domains": h.captchaDomainMgr.GetKnownDomains(),
	}
}
