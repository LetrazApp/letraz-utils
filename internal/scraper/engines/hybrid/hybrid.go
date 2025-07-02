package hybrid

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/llm"
	"letraz-scrapper/internal/scraper/engines/firecrawl"
	"letraz-scrapper/internal/scraper/engines/headed"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"
)

// HybridScraper implements a hybrid approach: try Rod scraper first, fallback to Firecrawl if a captcha is detected
type HybridScraper struct {
	config           *config.Config
	llmManager       *llm.Manager
	rodScraper       *headed.RodScraper
	firecrawlScraper *firecrawl.FirecrawlScraper
	logger           *logrus.Logger
}

// NewHybridScraper creates a new hybrid scraper instance
func NewHybridScraper(cfg *config.Config, llmManager *llm.Manager) *HybridScraper {
	logger := utils.GetLogger()

	// Initialize both scrapers
	rodScraper := headed.NewRodScraper(cfg, llmManager)
	firecrawlScraper := firecrawl.NewFirecrawlScraper(cfg, llmManager)

	if rodScraper == nil {
		logger.Error("Failed to initialize Rod scraper for hybrid engine")
		return nil
	}

	if firecrawlScraper == nil {
		logger.Error("Failed to initialize Firecrawl scraper for hybrid engine")
		return nil
	}

	logger.Info("Hybrid scraper initialized with Rod (primary) and Firecrawl (fallback)")

	return &HybridScraper{
		config:           cfg,
		llmManager:       llmManager,
		rodScraper:       rodScraper,
		firecrawlScraper: firecrawlScraper,
		logger:           logger,
	}
}

// ScrapeJob scrapes a job posting using hybrid approach: Rod first, Firecrawl on captcha
func (h *HybridScraper) ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error) {
	h.logger.WithField("url", url).Info("Starting hybrid job scraping (Rod → Firecrawl fallback)")

	// Try Rod scraper first
	h.logger.WithField("url", url).Info("Attempting scrape with Rod engine")
	job, err := h.rodScraper.ScrapeJob(ctx, url, options)

	// Check if it's a captcha error - if so, fallback to Firecrawl
	if err != nil {
		if customErr, ok := err.(*utils.CustomError); ok && customErr.Code == http.StatusTemporaryRedirect {
			h.logger.WithFields(logrus.Fields{
				"url":    url,
				"reason": customErr.Detail,
			}).Info("Rod scraper detected captcha, falling back to Firecrawl")

			// Fallback to Firecrawl
			h.logger.WithField("url", url).Info("Attempting scrape with Firecrawl engine")
			job, err = h.firecrawlScraper.ScrapeJob(ctx, url, options)

			if err != nil {
				h.logger.WithFields(logrus.Fields{
					"url":   url,
					"error": err.Error(),
				}).Error("Firecrawl fallback also failed")

				// Don't wrap CustomError types so they can be properly handled upstream
				if _, ok := err.(*utils.CustomError); ok {
					return nil, err
				}
				return nil, fmt.Errorf("hybrid scraping failed - Rod: captcha detected, Firecrawl: %w", err)
			}

			h.logger.WithFields(logrus.Fields{
				"url":       url,
				"job_title": job.Title,
				"company":   job.CompanyName,
				"engine":    "firecrawl_fallback",
			}).Info("Successfully scraped job using Firecrawl fallback")
			return job, nil
		}

		// Non-captcha error from Rod scraper - preserve CustomError types
		h.logger.WithFields(logrus.Fields{
			"url":   url,
			"error": err.Error(),
		}).Error("Rod scraper failed with non-captcha error")

		// Don't wrap CustomError types so they can be properly handled upstream
		if _, ok := err.(*utils.CustomError); ok {
			return nil, err
		}
		return nil, fmt.Errorf("rod scraper failed: %w", err)
	}

	// Rod scraper succeeded without captcha
	h.logger.WithFields(logrus.Fields{
		"url":       url,
		"job_title": job.Title,
		"company":   job.CompanyName,
		"engine":    "rod_primary",
	}).Info("Successfully scraped job using Rod engine (no captcha)")
	return job, nil
}

// ScrapeJobLegacy scrapes a job posting using legacy approach
func (h *HybridScraper) ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error) {
	h.logger.WithField("url", url).Info("Starting hybrid legacy job scraping")

	// Try Rod scraper first for legacy scraping
	jobPosting, err := h.rodScraper.ScrapeJobLegacy(ctx, url, options)

	// For legacy scraping, we don't expect captcha errors since it doesn't use LLM
	// But if there are issues, we can still fallback to Firecrawl
	if err != nil {
		h.logger.WithFields(logrus.Fields{
			"url":   url,
			"error": err.Error(),
		}).Info("Rod legacy scraper failed, falling back to Firecrawl")

		// Fallback to Firecrawl legacy
		jobPosting, err = h.firecrawlScraper.ScrapeJobLegacy(ctx, url, options)
		if err != nil {
			// Don't wrap CustomError types so they can be properly handled upstream
			if _, ok := err.(*utils.CustomError); ok {
				return nil, err
			}
			return nil, fmt.Errorf("hybrid legacy scraping failed - both Rod and Firecrawl failed: %w", err)
		}

		h.logger.WithField("url", url).Info("Successfully scraped job using Firecrawl legacy fallback")
	} else {
		h.logger.WithField("url", url).Info("Successfully scraped job using Rod legacy")
	}

	return jobPosting, nil
}

// Cleanup releases any resources used by both scrapers
func (h *HybridScraper) Cleanup() {
	h.logger.Info("Cleaning up hybrid scraper resources")

	if h.rodScraper != nil {
		h.rodScraper.Cleanup()
	}

	if h.firecrawlScraper != nil {
		h.firecrawlScraper.Cleanup()
	}
}

// IsHealthy checks if both scrapers are healthy
func (h *HybridScraper) IsHealthy() bool {
	rodHealthy := h.rodScraper != nil && h.rodScraper.IsHealthy()
	firecrawlHealthy := h.firecrawlScraper != nil && h.firecrawlScraper.IsHealthy()

	h.logger.WithFields(logrus.Fields{
		"rod_healthy":       rodHealthy,
		"firecrawl_healthy": firecrawlHealthy,
	}).Debug("Hybrid scraper health check")

	// As long as at least one scraper is healthy, we consider the hybrid healthy
	// Firecrawl is more critical since it's our fallback
	return firecrawlHealthy && rodHealthy
}
