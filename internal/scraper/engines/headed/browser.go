package headed

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
	"letraz-scrapper/internal/config"
	"letraz-scrapper/pkg/utils"
)

// BrowserManager manages browser instances and pools
type BrowserManager struct {
	config       *config.Config
	launcher     *launcher.Launcher
	browsers     []*rod.Browser
	mu           sync.RWMutex
	maxInstances int
	logger       *logrus.Logger
}

// BrowserInstance represents a managed browser instance
type BrowserInstance struct {
	Browser   *rod.Browser
	Page      *rod.Page
	manager   *BrowserManager
	createdAt time.Time
	inUse     bool
}

// NewBrowserManager creates a new browser manager
func NewBrowserManager(cfg *config.Config) *BrowserManager {
	logger := utils.GetLogger()

	// Setup launcher with stealth mode
	l := launcher.New().
		Headless(cfg.Scraper.HeadlessMode).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-web-security").
		Set("disable-features", "VizDisplayCompositor")

	if cfg.Scraper.UserAgent != "" {
		l = l.Set("user-agent", cfg.Scraper.UserAgent)
	}

	return &BrowserManager{
		config:       cfg,
		launcher:     l,
		browsers:     make([]*rod.Browser, 0),
		maxInstances: cfg.Workers.PoolSize,
		logger:       logger,
	}
}

// GetBrowser returns an available browser instance
func (bm *BrowserManager) GetBrowser(ctx context.Context) (*BrowserInstance, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Try to find an available browser
	for _, browser := range bm.browsers {
		// Check if browser is still connected by trying to get a page
		if bm.isBrowserHealthy(browser) {
			page, err := bm.createStealthPage(browser)
			if err != nil {
				bm.logger.WithError(err).Warn("Failed to create page from existing browser")
				continue
			}

			return &BrowserInstance{
				Browser:   browser,
				Page:      page,
				manager:   bm,
				createdAt: time.Now(),
				inUse:     true,
			}, nil
		}
	}

	// Create new browser if under limit
	if len(bm.browsers) < bm.maxInstances {
		browser, err := bm.createBrowser(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create browser: %w", err)
		}

		page, err := bm.createStealthPage(browser)
		if err != nil {
			browser.MustClose()
			return nil, fmt.Errorf("failed to create stealth page: %w", err)
		}

		bm.browsers = append(bm.browsers, browser)

		return &BrowserInstance{
			Browser:   browser,
			Page:      page,
			manager:   bm,
			createdAt: time.Now(),
			inUse:     true,
		}, nil
	}

	return nil, fmt.Errorf("browser pool exhausted, max instances: %d", bm.maxInstances)
}

// createBrowser creates a new browser instance
func (bm *BrowserManager) createBrowser(ctx context.Context) (*rod.Browser, error) {
	// Launch browser
	url, err := bm.launcher.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	// Connect to browser
	browser := rod.New().ControlURL(url)

	err = browser.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	bm.logger.Info("New browser instance created")
	return browser, nil
}

// createStealthPage creates a new page with stealth mode enabled
func (bm *BrowserManager) createStealthPage(browser *rod.Browser) (*rod.Page, error) {
	page, err := stealth.Page(browser)
	if err != nil {
		return nil, fmt.Errorf("failed to create stealth page: %w", err)
	}

	// Set viewport to common desktop resolution
	err = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             1920,
		Height:            1080,
		DeviceScaleFactor: 1,
	})
	if err != nil {
		bm.logger.WithError(err).Warn("Failed to set viewport")
	}

	// Set user agent if configured
	if bm.config.Scraper.UserAgent != "" {
		err = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent: bm.config.Scraper.UserAgent,
		})
		if err != nil {
			bm.logger.WithError(err).Warn("Failed to set user agent")
		}
	}

	return page, nil
}

// ReleaseBrowser releases a browser instance back to the pool
func (bi *BrowserInstance) Release() {
	if bi.Page != nil {
		bi.Page.MustClose()
	}
	bi.inUse = false
	bi.manager.logger.Debug("Browser instance released")
}

// Navigate navigates the page to the specified URL with timeout
func (bi *BrowserInstance) Navigate(ctx context.Context, url string, timeout time.Duration) error {
	// Set navigation timeout
	navCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Navigate to URL
	err := rod.Try(func() {
		bi.Page.Context(navCtx).MustNavigate(url).MustWaitLoad()
	})

	if err != nil {
		return fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	bi.manager.logger.WithField("url", url).Debug("Successfully navigated to URL")
	return nil
}

// GetPageHTML returns the full HTML content of the current page
func (bi *BrowserInstance) GetPageHTML() (string, error) {
	html, err := bi.Page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get page HTML: %w", err)
	}
	return html, nil
}

// WaitForSelector waits for an element to appear on the page
func (bi *BrowserInstance) WaitForSelector(selector string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := rod.Try(func() {
		bi.Page.Context(ctx).MustElement(selector)
	})

	if err != nil {
		return fmt.Errorf("element with selector '%s' not found within timeout: %w", selector, err)
	}

	return nil
}

// isBrowserHealthy checks if a browser instance is still healthy
func (bm *BrowserManager) isBrowserHealthy(browser *rod.Browser) bool {
	err := rod.Try(func() {
		browser.MustPages()
	})
	return err == nil
}

// Cleanup closes all browser instances and launchers
func (bm *BrowserManager) Cleanup() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, browser := range bm.browsers {
		if bm.isBrowserHealthy(browser) {
			browser.MustClose()
		}
	}

	bm.browsers = nil
	bm.launcher.Cleanup()
	bm.logger.Info("Browser manager cleanup completed")
}

// IsHealthy checks if the browser manager is healthy
func (bm *BrowserManager) IsHealthy() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	activeBrowsers := 0
	for _, browser := range bm.browsers {
		if bm.isBrowserHealthy(browser) {
			activeBrowsers++
		}
	}

	return activeBrowsers >= 0 // At least one browser should be available
}
