package headed

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// GlobalBrowserPool manages a shared pool of browser instances across the entire application
type GlobalBrowserPool struct {
	config            *config.Config
	launcher          *launcher.Launcher
	browsers          []*ManagedBrowser
	availableBrowsers chan *ManagedBrowser
	mu                sync.RWMutex
	maxInstances      int
	currentInstances  int
	logger            types.Logger
	ctx               context.Context
	cancel            context.CancelFunc
	cleanupTicker     *time.Ticker
	metrics           *BrowserPoolMetrics
}

// ManagedBrowser represents a browser instance with lifecycle management
type ManagedBrowser struct {
	Browser     *rod.Browser
	ID          string
	CreatedAt   time.Time
	LastUsedAt  time.Time
	InUse       bool
	UsageCount  int
	MaxIdleTime time.Duration
	mu          sync.RWMutex
}

// BrowserPoolMetrics tracks browser pool statistics
type BrowserPoolMetrics struct {
	mu                     sync.RWMutex
	TotalBrowsersCreated   int64
	TotalBrowsersClosed    int64
	CurrentActiveBrowsers  int64
	AvailableBrowsers      int64
	QueuedRequests         int64
	AverageAcquisitionTime time.Duration
}

// GlobalBrowserInstance represents a browser instance with a page for use
type GlobalBrowserInstance struct {
	Browser   *ManagedBrowser
	Page      *rod.Page
	pool      *GlobalBrowserPool
	createdAt time.Time
}

var (
	globalPool *GlobalBrowserPool
	poolOnce   sync.Once
)

// InitializeGlobalBrowserPool initializes the global browser pool (should be called once at startup)
func InitializeGlobalBrowserPool(cfg *config.Config) error {
	var initErr error
	poolOnce.Do(func() {
		logger := logging.GetGlobalLogger()

		// Calculate max instances based on system resources and configuration
		maxInstances := calculateOptimalBrowserInstances(cfg)

		// Setup launcher with enhanced stealth mode and optimized rendering
		l := launcher.New().
			Headless(cfg.Scraper.HeadlessMode).
			NoSandbox(true).
			Set("disable-blink-features", "AutomationControlled").
			Set("disable-web-security").
			Set("disable-background-timer-throttling").
			Set("disable-backgrounding-occluded-windows").
			Set("disable-renderer-backgrounding").
			Set("disable-dev-shm-usage").        // Reduce memory usage
			Set("disable-gpu").                  // Reduce GPU usage for screenshots
			Set("no-first-run").                 // Skip first run wizards
			Set("no-default-browser-check").     // Skip default browser checks
			Set("disable-background-networking") // Reduce background activity

		// Use system-installed Chrome/Chromium instead of downloading
		if chromePath := getSystemChromePath(); chromePath != "" {
			l = l.Bin(chromePath)
			logger.Info("Global browser pool using system Chrome", map[string]interface{}{
				"chrome_path":   chromePath,
				"max_instances": maxInstances,
			})
		} else {
			logger.Warn("System Chrome not found, Rod will download browser", map[string]interface{}{
				"max_instances": maxInstances,
			})
		}

		if cfg.Scraper.UserAgent != "" {
			l = l.Set("user-agent", cfg.Scraper.UserAgent)
		}

		ctx, cancel := context.WithCancel(context.Background())

		globalPool = &GlobalBrowserPool{
			config:            cfg,
			launcher:          l,
			browsers:          make([]*ManagedBrowser, 0, maxInstances),
			availableBrowsers: make(chan *ManagedBrowser, maxInstances),
			maxInstances:      maxInstances,
			currentInstances:  0,
			logger:            logger,
			ctx:               ctx,
			cancel:            cancel,
			metrics:           &BrowserPoolMetrics{},
		}

		// Start background cleanup routine
		globalPool.startCleanupRoutine()

		logger.Info("Global browser pool initialized", map[string]interface{}{
			"max_instances": maxInstances,
		})
	})

	if globalPool == nil {
		return fmt.Errorf("failed to initialize global browser pool")
	}

	return initErr
}

// GetGlobalBrowserPool returns the global browser pool instance
func GetGlobalBrowserPool() (*GlobalBrowserPool, error) {
	if globalPool == nil {
		return nil, fmt.Errorf("global browser pool not initialized - call InitializeGlobalBrowserPool first")
	}
	return globalPool, nil
}

// AcquireBrowser gets a browser instance with timeout
func (gbp *GlobalBrowserPool) AcquireBrowser(ctx context.Context) (*GlobalBrowserInstance, error) {
	startTime := time.Now()
	gbp.metrics.mu.Lock()
	gbp.metrics.QueuedRequests++
	gbp.metrics.mu.Unlock()

	defer func() {
		gbp.metrics.mu.Lock()
		gbp.metrics.QueuedRequests--
		gbp.metrics.AverageAcquisitionTime = time.Since(startTime)
		gbp.metrics.mu.Unlock()
	}()

	// Try to get an available browser from the pool with a shorter wait
	select {
	case managedBrowser := <-gbp.availableBrowsers:
		if gbp.isManagedBrowserHealthy(managedBrowser) {
			return gbp.createGlobalInstance(managedBrowser)
		}
		// Browser is unhealthy, continue to create new one
		gbp.closeManagedBrowser(managedBrowser)
	case <-time.After(1 * time.Second):
		// Quick timeout waiting for available browser, try to create new one
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Create new browser if under limit - use independent context to avoid cancellation
	gbp.mu.Lock()
	if gbp.currentInstances < gbp.maxInstances {
		gbp.currentInstances++
		gbp.mu.Unlock()

		// Use background context for browser creation to avoid premature cancellation
		managedBrowser, err := gbp.createManagedBrowser(context.Background())
		if err != nil {
			gbp.mu.Lock()
			gbp.currentInstances--
			gbp.mu.Unlock()
			return nil, fmt.Errorf("failed to create browser: %w", err)
		}

		return gbp.createGlobalInstance(managedBrowser)
	}
	gbp.mu.Unlock()

	// Pool exhausted, wait with timeout
	gbp.logger.Warn("Browser pool exhausted, waiting for available instance", map[string]interface{}{
		"max_instances":     gbp.maxInstances,
		"current_instances": gbp.currentInstances,
	})

	// Create a separate timeout context for pool waiting
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case managedBrowser := <-gbp.availableBrowsers:
		if gbp.isManagedBrowserHealthy(managedBrowser) {
			return gbp.createGlobalInstance(managedBrowser)
		}
		gbp.closeManagedBrowser(managedBrowser)
		return nil, fmt.Errorf("acquired unhealthy browser, pool needs cleanup")
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timeout waiting for browser instance")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ReleaseBrowser returns a browser instance to the pool
func (gbi *GlobalBrowserInstance) Release() {
	if gbi.Page != nil {
		// Close the page but keep the browser
		_ = gbi.Page.Close()
	}

	managedBrowser := gbi.Browser
	managedBrowser.mu.Lock()
	managedBrowser.InUse = false
	managedBrowser.LastUsedAt = time.Now()
	managedBrowser.UsageCount++
	managedBrowser.mu.Unlock()

	// Return browser to available pool
	select {
	case gbi.pool.availableBrowsers <- managedBrowser:
		gbi.pool.logger.Debug("Browser returned to pool", map[string]interface{}{
			"browser_id":  managedBrowser.ID,
			"usage_count": managedBrowser.UsageCount,
		})
	default:
		// Pool is full, close the browser
		gbi.pool.logger.Warn("Browser pool full, closing browser", map[string]interface{}{
			"browser_id": managedBrowser.ID,
		})
		gbi.pool.closeManagedBrowser(managedBrowser)
	}
}

// createManagedBrowser creates a new managed browser instance
func (gbp *GlobalBrowserPool) createManagedBrowser(ctx context.Context) (*ManagedBrowser, error) {
	// Use a longer timeout for browser creation to avoid premature cancellation
	browserCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Launch browser with extended timeout
	url, err := gbp.launcher.Context(browserCtx).Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	// Connect to browser with timeout
	browser := rod.New().Context(browserCtx).ControlURL(url)
	err = browser.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	browserID := fmt.Sprintf("browser-%d", time.Now().UnixNano())
	managedBrowser := &ManagedBrowser{
		Browser:     browser,
		ID:          browserID,
		CreatedAt:   time.Now(),
		LastUsedAt:  time.Now(),
		InUse:       false,
		UsageCount:  0,
		MaxIdleTime: 5 * time.Minute, // Close browsers idle for 5 minutes
	}

	gbp.mu.Lock()
	gbp.browsers = append(gbp.browsers, managedBrowser)
	gbp.mu.Unlock()

	gbp.metrics.mu.Lock()
	gbp.metrics.TotalBrowsersCreated++
	gbp.metrics.CurrentActiveBrowsers++
	gbp.metrics.mu.Unlock()

	gbp.logger.Info("New managed browser created", map[string]interface{}{
		"browser_id":        browserID,
		"current_instances": gbp.currentInstances,
	})

	return managedBrowser, nil
}

// createGlobalInstance creates a GlobalBrowserInstance with a new page
func (gbp *GlobalBrowserPool) createGlobalInstance(managedBrowser *ManagedBrowser) (*GlobalBrowserInstance, error) {
	// Use a fresh context for page creation to avoid cancellation issues
	pageCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	page, err := gbp.createStealthPageWithContext(pageCtx, managedBrowser.Browser)
	if err != nil {
		gbp.closeManagedBrowser(managedBrowser)
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	managedBrowser.mu.Lock()
	managedBrowser.InUse = true
	managedBrowser.LastUsedAt = time.Now()
	managedBrowser.mu.Unlock()

	return &GlobalBrowserInstance{
		Browser:   managedBrowser,
		Page:      page,
		pool:      gbp,
		createdAt: time.Now(),
	}, nil
}

// createStealthPage creates a new page with stealth mode enabled (legacy method)
func (gbp *GlobalBrowserPool) createStealthPage(browser *rod.Browser) (*rod.Page, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return gbp.createStealthPageWithContext(ctx, browser)
}

// createStealthPageWithContext creates a new page with stealth mode enabled using provided context
func (gbp *GlobalBrowserPool) createStealthPageWithContext(ctx context.Context, browser *rod.Browser) (*rod.Page, error) {
	// Create page with context timeout
	page, err := browser.Context(ctx).Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	// Set user agent if configured
	if gbp.config.Scraper.UserAgent != "" {
		err = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent: gbp.config.Scraper.UserAgent,
		})
		if err != nil {
			page.MustClose()
			gbp.logger.Warn("Failed to set user agent on stealth page", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Apply stealth mode patches manually since stealth.Page() might have context issues
	err = gbp.applyStealthPatches(ctx, page)
	if err != nil {
		gbp.logger.Warn("Failed to apply stealth patches", map[string]interface{}{
			"error": err.Error(),
		})
		// Continue without stealth patches rather than failing
	}

	return page, nil
}

// applyStealthPatches applies stealth mode JavaScript patches to a page
func (gbp *GlobalBrowserPool) applyStealthPatches(ctx context.Context, page *rod.Page) error {
	// Apply basic stealth JavaScript with timeout
	stealthJS := `() => {
		// Override webdriver property
		Object.defineProperty(navigator, 'webdriver', {
			get: () => undefined,
		});
		
		// Override automation-related properties
		Object.defineProperty(navigator, 'plugins', {
			get: () => [1, 2, 3, 4, 5],
		});
		
		Object.defineProperty(navigator, 'languages', {
			get: () => ['en-US', 'en'],
		});
		
		// Override chrome property
		window.chrome = {
			runtime: {},
		};
		
		// Override permissions
		const originalQuery = window.navigator.permissions.query;
		window.navigator.permissions.query = (parameters) => (
			parameters.name === 'notifications' ?
				Promise.resolve({ state: Notification.permission }) :
				originalQuery(parameters)
		);
	}`

	_, err := page.Context(ctx).Eval(stealthJS)
	return err
}

// isManagedBrowserHealthy checks if a managed browser is still healthy
func (gbp *GlobalBrowserPool) isManagedBrowserHealthy(managedBrowser *ManagedBrowser) bool {
	if managedBrowser.Browser == nil {
		return false
	}

	// Check if browser process is still alive
	_, err := managedBrowser.Browser.Pages()
	return err == nil
}

// closeManagedBrowser closes a managed browser and removes it from tracking
func (gbp *GlobalBrowserPool) closeManagedBrowser(managedBrowser *ManagedBrowser) {
	if managedBrowser.Browser != nil {
		// Force close with timeout to prevent hanging
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Try graceful close first
		err := managedBrowser.Browser.Close()
		if err != nil {
			gbp.logger.Warn("Failed to gracefully close browser, forcing close", map[string]interface{}{
				"browser_id": managedBrowser.ID,
				"error":      err.Error(),
			})

			// Force close if graceful close fails
			managedBrowser.Browser.MustClose()
		}

		// Wait for close with timeout
		done := make(chan bool, 1)
		go func() {
			// Check if browser process is actually closed
			for i := 0; i < 10; i++ {
				if !gbp.isManagedBrowserHealthy(managedBrowser) {
					done <- true
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			done <- false
		}()

		select {
		case closed := <-done:
			if !closed {
				gbp.logger.Error("Browser process did not close within timeout", map[string]interface{}{
					"browser_id": managedBrowser.ID,
				})
			}
		case <-closeCtx.Done():
			gbp.logger.Error("Browser close operation timed out", map[string]interface{}{
				"browser_id": managedBrowser.ID,
			})
		}
	}

	// Remove from browsers slice
	gbp.mu.Lock()
	for i, browser := range gbp.browsers {
		if browser.ID == managedBrowser.ID {
			gbp.browsers = append(gbp.browsers[:i], gbp.browsers[i+1:]...)
			break
		}
	}
	gbp.currentInstances--
	gbp.mu.Unlock()

	gbp.metrics.mu.Lock()
	gbp.metrics.TotalBrowsersClosed++
	gbp.metrics.CurrentActiveBrowsers--
	gbp.metrics.mu.Unlock()

	gbp.logger.Info("Managed browser closed", map[string]interface{}{
		"browser_id":        managedBrowser.ID,
		"current_instances": gbp.currentInstances,
		"usage_count":       managedBrowser.UsageCount,
	})
}

// startCleanupRoutine starts background cleanup of idle browsers
func (gbp *GlobalBrowserPool) startCleanupRoutine() {
	gbp.cleanupTicker = time.NewTicker(1 * time.Minute)

	go func() {
		defer gbp.cleanupTicker.Stop()

		for {
			select {
			case <-gbp.cleanupTicker.C:
				gbp.cleanupIdleBrowsers()
			case <-gbp.ctx.Done():
				return
			}
		}
	}()
}

// cleanupIdleBrowsers removes browsers that have been idle too long
func (gbp *GlobalBrowserPool) cleanupIdleBrowsers() {
	now := time.Now()
	var browsersToClose []*ManagedBrowser
	var unhealthyBrowsers []*ManagedBrowser

	gbp.mu.RLock()
	for _, browser := range gbp.browsers {
		browser.mu.RLock()
		isIdle := !browser.InUse && now.Sub(browser.LastUsedAt) > browser.MaxIdleTime
		isStuck := browser.InUse && now.Sub(browser.LastUsedAt) > 10*time.Minute
		browser.mu.RUnlock()

		if isIdle {
			browsersToClose = append(browsersToClose, browser)
		} else if isStuck || !gbp.isManagedBrowserHealthy(browser) {
			unhealthyBrowsers = append(unhealthyBrowsers, browser)
		}
	}
	gbp.mu.RUnlock()

	// Close idle browsers
	for _, browser := range browsersToClose {
		gbp.logger.Info("Closing idle browser", map[string]interface{}{
			"browser_id": browser.ID,
			"idle_time":  now.Sub(browser.LastUsedAt),
		})
		gbp.closeManagedBrowser(browser)
	}

	// Close unhealthy/stuck browsers
	for _, browser := range unhealthyBrowsers {
		gbp.logger.Warn("Closing unhealthy/stuck browser", map[string]interface{}{
			"browser_id": browser.ID,
			"in_use":     browser.InUse,
			"last_used":  browser.LastUsedAt,
		})
		gbp.closeManagedBrowser(browser)
	}

	totalClosed := len(browsersToClose) + len(unhealthyBrowsers)
	if totalClosed > 0 {
		gbp.logger.Info("Browser cleanup completed", map[string]interface{}{
			"idle_closed":        len(browsersToClose),
			"unhealthy_closed":   len(unhealthyBrowsers),
			"total_closed":       totalClosed,
			"remaining_browsers": gbp.currentInstances,
		})
	}

	// Log metrics every 5 minutes for monitoring
	if now.Minute()%5 == 0 && now.Second() < 10 {
		metrics := gbp.GetMetrics()
		gbp.logger.Info("Browser pool status", map[string]interface{}{
			"active_browsers":    metrics.CurrentActiveBrowsers,
			"available_browsers": metrics.AvailableBrowsers,
			"queued_requests":    metrics.QueuedRequests,
			"total_created":      metrics.TotalBrowsersCreated,
			"total_closed":       metrics.TotalBrowsersClosed,
		})
	}
}

// GetMetrics returns current browser pool metrics
func (gbp *GlobalBrowserPool) GetMetrics() *BrowserPoolMetrics {
	gbp.metrics.mu.RLock()
	defer gbp.metrics.mu.RUnlock()

	return &BrowserPoolMetrics{
		TotalBrowsersCreated:   gbp.metrics.TotalBrowsersCreated,
		TotalBrowsersClosed:    gbp.metrics.TotalBrowsersClosed,
		CurrentActiveBrowsers:  gbp.metrics.CurrentActiveBrowsers,
		AvailableBrowsers:      int64(len(gbp.availableBrowsers)),
		QueuedRequests:         gbp.metrics.QueuedRequests,
		AverageAcquisitionTime: gbp.metrics.AverageAcquisitionTime,
	}
}

// ForceCleanupStuckBrowsers forcefully closes browsers that may be stuck
func (gbp *GlobalBrowserPool) ForceCleanupStuckBrowsers() {
	gbp.logger.Info("Starting force cleanup of stuck browsers")

	var stuckBrowsers []*ManagedBrowser
	now := time.Now()

	gbp.mu.RLock()
	for _, browser := range gbp.browsers {
		browser.mu.RLock()
		// Consider browsers stuck if they've been in use for more than 10 minutes
		isStuck := browser.InUse && now.Sub(browser.LastUsedAt) > 10*time.Minute
		browser.mu.RUnlock()

		if isStuck || !gbp.isManagedBrowserHealthy(browser) {
			stuckBrowsers = append(stuckBrowsers, browser)
		}
	}
	gbp.mu.RUnlock()

	for _, browser := range stuckBrowsers {
		gbp.logger.Warn("Force closing stuck browser", map[string]interface{}{
			"browser_id": browser.ID,
			"in_use":     browser.InUse,
			"last_used":  browser.LastUsedAt,
		})
		gbp.closeManagedBrowser(browser)
	}

	if len(stuckBrowsers) > 0 {
		gbp.logger.Info("Force cleanup completed", map[string]interface{}{
			"closed_browsers": len(stuckBrowsers),
		})
	}
}

// Shutdown gracefully shuts down the global browser pool
func (gbp *GlobalBrowserPool) Shutdown(ctx context.Context) error {
	gbp.logger.Info("Shutting down global browser pool")

	// Stop cleanup routine
	gbp.cancel()

	// Wait for cleanup routine to stop
	if gbp.cleanupTicker != nil {
		gbp.cleanupTicker.Stop()
	}

	// Force cleanup any stuck browsers first
	gbp.ForceCleanupStuckBrowsers()

	// Close remaining browsers with timeout
	shutdownComplete := make(chan bool, 1)
	go func() {
		gbp.mu.Lock()
		browsers := make([]*ManagedBrowser, len(gbp.browsers))
		copy(browsers, gbp.browsers)
		gbp.mu.Unlock()

		for _, browser := range browsers {
			gbp.closeManagedBrowser(browser)
		}

		shutdownComplete <- true
	}()

	// Wait for shutdown with timeout
	select {
	case <-shutdownComplete:
		gbp.logger.Info("All browsers closed gracefully")
	case <-ctx.Done():
		gbp.logger.Warn("Browser shutdown timed out, some browsers may still be running")
	case <-time.After(30 * time.Second):
		gbp.logger.Warn("Browser shutdown took too long, forcing completion")
	}

	// Clean up launcher
	gbp.launcher.Cleanup()

	gbp.logger.Info("Global browser pool shutdown completed", map[string]interface{}{
		"remaining_browsers": gbp.currentInstances,
	})

	return nil
}

// IsHealthy checks if the global browser pool is healthy
func (gbp *GlobalBrowserPool) IsHealthy() bool {
	gbp.mu.RLock()
	defer gbp.mu.RUnlock()

	return gbp.ctx.Err() == nil && gbp.currentInstances >= 0
}

// calculateOptimalBrowserInstances calculates optimal number of browser instances
func calculateOptimalBrowserInstances(cfg *config.Config) int {
	// Conservative approach: limit to 5 browsers max regardless of worker pool size
	// Each browser can handle multiple pages sequentially
	maxBrowsers := 5

	// Scale down for lower worker counts
	if cfg.Workers.PoolSize < 5 {
		maxBrowsers = cfg.Workers.PoolSize
	}

	// Minimum of 2 browsers for redundancy
	if maxBrowsers < 2 {
		maxBrowsers = 2
	}

	return maxBrowsers
}
