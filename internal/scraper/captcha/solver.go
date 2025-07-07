package captcha

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/2captcha/2captcha-go"
	"github.com/sirupsen/logrus"
	"letraz-utils/internal/config"
	"letraz-utils/pkg/utils"
)

// CaptchaSolver interface for different captcha solving services
type CaptchaSolver interface {
	SolveRecaptcha(ctx context.Context, siteKey, pageURL string) (string, error)
	SolveTurnstile(ctx context.Context, siteKey, pageURL string) (string, error)
	IsHealthy() bool
}

// TwoCaptchaSolver implements 2CAPTCHA service integration using official library
type TwoCaptchaSolver struct {
	config *config.Config
	client *api2captcha.Client
	logger *logrus.Logger
}

// NewTwoCaptchaSolver creates a new 2CAPTCHA solver instance
func NewTwoCaptchaSolver(cfg *config.Config) *TwoCaptchaSolver {
	logger := utils.GetLogger().WithField("component", "2captcha").Logger

	if cfg.Scraper.Captcha.APIKey == "" {
		logger.Warn("2CAPTCHA API key not configured - captcha solving will be disabled")
	} else {
		logger.WithField("api_key_length", len(cfg.Scraper.Captcha.APIKey)).Info("2CAPTCHA solver initialized with API key")
	}

	client := api2captcha.NewClient(cfg.Scraper.Captcha.APIKey)

	// Configure timeouts
	client.DefaultTimeout = int(cfg.Scraper.Captcha.Timeout.Seconds())
	client.RecaptchaTimeout = int(cfg.Scraper.Captcha.Timeout.Seconds())
	client.PollingInterval = 5 // Check every 5 seconds

	logger.WithFields(logrus.Fields{
		"default_timeout":   client.DefaultTimeout,
		"recaptcha_timeout": client.RecaptchaTimeout,
		"polling_interval":  client.PollingInterval,
		"enable_auto_solve": cfg.Scraper.Captcha.EnableAutoSolve,
	}).Info("2CAPTCHA client configured")

	return &TwoCaptchaSolver{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// SolveRecaptcha solves a reCAPTCHA challenge using 2CAPTCHA service
func (tcs *TwoCaptchaSolver) SolveRecaptcha(ctx context.Context, siteKey, pageURL string) (string, error) {
	if !tcs.config.Scraper.Captcha.EnableAutoSolve {
		return "", fmt.Errorf("captcha auto-solve is disabled")
	}

	if tcs.config.Scraper.Captcha.APIKey == "" {
		return "", fmt.Errorf("2CAPTCHA API key not configured")
	}

	tcs.logger.WithFields(logrus.Fields{
		"site_key": siteKey,
		"page_url": pageURL,
	}).Info("Starting reCAPTCHA solving with 2CAPTCHA")

	startTime := time.Now()

	// Create reCAPTCHA v2 task
	captcha := api2captcha.ReCaptcha{
		SiteKey: siteKey,
		Url:     pageURL,
	}

	// Solve the captcha
	req := captcha.ToRequest()
	code, _, err := tcs.client.Solve(req)
	if err != nil {
		tcs.logger.WithFields(logrus.Fields{
			"site_key": siteKey,
			"page_url": pageURL,
			"error":    err.Error(),
		}).Error("Failed to solve reCAPTCHA")
		return "", fmt.Errorf("failed to solve reCAPTCHA: %w", err)
	}

	solvingTime := time.Since(startTime)
	tcs.logger.WithFields(logrus.Fields{
		"site_key":     siteKey,
		"page_url":     pageURL,
		"solving_time": solvingTime,
	}).Info("Successfully solved reCAPTCHA")

	return code, nil
}

// SolveTurnstile solves a Cloudflare Turnstile challenge using 2CAPTCHA service
func (tcs *TwoCaptchaSolver) SolveTurnstile(ctx context.Context, siteKey, pageURL string) (string, error) {
	if !tcs.config.Scraper.Captcha.EnableAutoSolve {
		return "", fmt.Errorf("captcha auto-solve is disabled")
	}

	if tcs.config.Scraper.Captcha.APIKey == "" {
		return "", fmt.Errorf("2CAPTCHA API key not configured")
	}

	tcs.logger.WithFields(logrus.Fields{
		"site_key": siteKey,
		"page_url": pageURL,
	}).Info("Starting Cloudflare Turnstile solving with 2CAPTCHA")

	startTime := time.Now()

	// Create Cloudflare Turnstile task
	captcha := api2captcha.CloudflareTurnstile{
		SiteKey: siteKey,
		Url:     pageURL,
	}

	// Solve the captcha
	req := captcha.ToRequest()
	code, captchaId, err := tcs.client.Solve(req)
	if err != nil {
		tcs.logger.WithFields(logrus.Fields{
			"site_key":   siteKey,
			"page_url":   pageURL,
			"captcha_id": captchaId,
			"error":      err.Error(),
			"error_type": fmt.Sprintf("%T", err),
		}).Error("Failed to solve Cloudflare Turnstile")
		return "", fmt.Errorf("failed to solve Cloudflare Turnstile: %w", err)
	}

	solvingTime := time.Since(startTime)
	tcs.logger.WithFields(logrus.Fields{
		"site_key":     siteKey,
		"page_url":     pageURL,
		"solving_time": solvingTime,
	}).Info("Successfully solved Cloudflare Turnstile")

	return code, nil
}

// IsHealthy checks if the 2CAPTCHA service is available
func (tcs *TwoCaptchaSolver) IsHealthy() bool {
	if tcs.config.Scraper.Captcha.APIKey == "" {
		tcs.logger.Debug("2CAPTCHA health check failed: no API key configured")
		return false
	}

	// Check balance to verify API key is working
	balance, err := tcs.client.GetBalance()
	if err != nil {
		tcs.logger.WithFields(logrus.Fields{
			"error":          err.Error(),
			"api_key_length": len(tcs.config.Scraper.Captcha.APIKey),
		}).Error("2CAPTCHA health check failed - API call error")
		return false
	}

	tcs.logger.WithField("balance", balance).Info("2CAPTCHA health check successful")
	return balance >= 0 // Allow zero balance for now
}

// DetectCaptcha detects if a page contains a captcha challenge
func DetectCaptcha(pageContent string) (bool, string, error) {
	pageContentLower := strings.ToLower(pageContent)

	// Check for reCAPTCHA v2
	if strings.Contains(pageContentLower, "g-recaptcha") || strings.Contains(pageContentLower, "recaptcha") {
		// Extract site key
		siteKey := extractRecaptchaSiteKey(pageContent)
		if siteKey != "" {
			return true, siteKey, nil
		}
	}

	// Check for Cloudflare Turnstile
	if strings.Contains(pageContentLower, "turnstile") || strings.Contains(pageContentLower, "cf-turnstile") {
		// Extract Turnstile site key
		siteKey := extractTurnstileSiteKey(pageContent)
		if siteKey != "" {
			return true, "turnstile:" + siteKey, nil
		}
	}

	// Check for Cloudflare challenge pages - comprehensive detection
	cloudflareIndicators := []string{
		"cf-challenge",
		"cloudflare",
		"just a moment",
		"please wait while we verify",
		"checking your browser",
		"ddos protection by cloudflare",
		"enable javascript and cookies",
		"security verification",
		"cf-browser-verification",
		"__cf_chl_jschl_tk__",
		"ray id",
		"performance & security by cloudflare",
	}

	for _, indicator := range cloudflareIndicators {
		if strings.Contains(pageContentLower, indicator) {
			// Try to extract Turnstile site key from Cloudflare pages
			if siteKey := extractTurnstileSiteKey(pageContent); siteKey != "" {
				return true, "turnstile:" + siteKey, nil
			}
			// If no specific Turnstile key found, mark as generic Cloudflare
			return true, "cloudflare", nil
		}
	}

	return false, "", nil
}

// extractRecaptchaSiteKey extracts the reCAPTCHA site key from HTML content
func extractRecaptchaSiteKey(html string) string {
	// Look for data-sitekey attribute
	patterns := []string{
		`data-sitekey="([^"]+)"`,
		`data-sitekey='([^']+)'`,
		`"sitekey"\s*:\s*"([^"]+)"`,
		`'sitekey'\s*:\s*'([^']+)'`,
	}

	for _, pattern := range patterns {
		if matches := utils.FindRegexMatch(html, pattern); len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// extractTurnstileSiteKey extracts the Cloudflare Turnstile site key from HTML content
func extractTurnstileSiteKey(html string) string {
	// Look for Turnstile data-sitekey attribute in various patterns
	patterns := []string{
		// Traditional data-sitekey patterns
		`data-sitekey="([^"]+)"[^>]*(?:turnstile|cf-turnstile)`,
		`(?:turnstile|cf-turnstile)[^>]*data-sitekey="([^"]+)"`,
		`<div[^>]*class="[^"]*cf-turnstile[^"]*"[^>]*data-sitekey="([^"]+)"`,
		`<div[^>]*data-sitekey="([^"]+)"[^>]*class="[^"]*cf-turnstile[^"]*"`,
		`window\.turnstile.*?sitekey['"]\s*:\s*['"]([^'"]+)['"]`,
		`turnstile\.render\([^)]*['"]([0-9a-zA-Z_-]{20,})['"]`,
		`cf-turnstile[^>]*data-sitekey=['"]([^'"]+)['"]`,
		`data-sitekey="([^"]+)".*?turnstile`,
		`turnstile.*?data-sitekey="([^"]+)"`,
		`cf-turnstile.*?data-sitekey="([^"]+)"`,
		`data-sitekey="([^"]+)".*?cf-turnstile`,
		`"sitekey"\s*:\s*"([^"]+)".*?turnstile`,
		`turnstile.*?"sitekey"\s*:\s*"([^"]+)"`,

		// Iframe-based Cloudflare challenge patterns
		`<iframe[^>]*src="[^"]*challenges\.cloudflare\.com[^"]*/(0x[0-9a-zA-Z_-]+)/[^"]*"`,
		`src="[^"]*challenges\.cloudflare\.com[^"]*/(0x[0-9a-zA-Z_-]+)/[^"]*"`,
		`challenges\.cloudflare\.com[^"]*/(0x[0-9a-zA-Z_-]+)/`,
		`challenges\.cloudflare\.com[^"]*rcv[^"]*/(0x[0-9a-zA-Z_-]+)/`,
		`https://challenges\.cloudflare\.com/[^"]*/(0x[0-9a-zA-Z_-]+)/[^"]*`,
		`cloudflare\.com[^"]*/(0x[0-9a-zA-Z_-]+)/`,
		`"(0x[0-9a-zA-Z_-]+)"[^"]*cloudflare`,
		`cloudflare[^"]*"(0x[0-9a-zA-Z_-]+)"`,
		// Specific pattern for the iframe structure seen in the screenshot
		`challenges\.cloudflare\.com/cdn-cgi/challenge-platform/[^"]*/(0x[0-9a-zA-Z_-]+)/`,
		// More general pattern for any 0x key in cloudflare context
		`challenges\.cloudflare\.com[^"]*?(0x[0-9a-zA-Z_-]{20,})[^"]*`,
	}

	for _, pattern := range patterns {
		if matches := utils.FindRegexMatch(html, pattern); len(matches) > 1 {
			siteKey := strings.TrimSpace(matches[1])
			if siteKey != "" && len(siteKey) > 10 { // Basic validation
				return siteKey
			}
		}
	}

	return ""
}

// IsCloudflareResolved checks if the Cloudflare challenge has been resolved
func IsCloudflareResolved(pageContent string) bool {
	pageContentLower := strings.ToLower(pageContent)

	// Check for indicators that we're still on a challenge page
	challengeIndicators := []string{
		"cf-challenge",
		"just a moment",
		"please wait while we verify",
		"checking your browser",
		"enable javascript and cookies",
		"security verification",
		"cf-browser-verification",
		"__cf_chl_jschl_tk__",
		"performance & security by cloudflare",
	}

	for _, indicator := range challengeIndicators {
		if strings.Contains(pageContentLower, indicator) {
			return false
		}
	}

	// Check for positive indicators that we're on the actual content
	contentIndicators := []string{
		"<title>",
		"job posting",
		"job description",
		"apply now",
		"company",
		"salary",
		"requirements",
		"<main",
		"<article",
		"<section",
	}

	indicatorCount := 0
	for _, indicator := range contentIndicators {
		if strings.Contains(pageContentLower, indicator) {
			indicatorCount++
		}
	}

	// If we have multiple content indicators and no challenge indicators, consider it resolved
	return indicatorCount >= 3
}
