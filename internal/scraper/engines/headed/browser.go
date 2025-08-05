package headed

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// BrowserManager manages browser instances and pools
type BrowserManager struct {
	config       *config.Config
	launcher     *launcher.Launcher
	browsers     []*rod.Browser
	mu           sync.RWMutex
	maxInstances int
	logger       types.Logger
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
	logger := logging.GetGlobalLogger()

	// Setup launcher with enhanced stealth mode and critical Docker flags
	l := launcher.New().
		Headless(cfg.Scraper.HeadlessMode).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-web-security").
		Set("disable-background-timer-throttling").    // Prevent render delays
		Set("disable-backgrounding-occluded-windows"). // Keep rendering active
		Set("disable-renderer-backgrounding").         // Prevent background throttling
		// Critical flags to fix Docker navigation errors
		Set("disable-gpu").          // Essential: prevents GPU context failures in Docker
		Set("disable-dev-shm-usage") // Essential: overcomes Docker shared memory limitations

	// Use system-installed Chrome/Chromium instead of downloading
	if chromePath := getSystemChromePath(); chromePath != "" {
		l = l.Bin(chromePath)
		logger.Info("Using system Chrome browser", map[string]interface{}{
			"chrome_path": chromePath,
		})
	} else {
		logger.Warn("System Chrome not found, Rod will download browser", map[string]interface{}{})
	}

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
				bm.logger.Warn("Failed to create page from existing browser", map[string]interface{}{
					"error": err.Error(),
				})
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

	bm.logger.Info("New browser instance created", map[string]interface{}{})
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
		bm.logger.Warn("Failed to set viewport", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Set user agent if configured
	if bm.config.Scraper.UserAgent != "" {
		err = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent: bm.config.Scraper.UserAgent,
		})
		if err != nil {
			bm.logger.Warn("Failed to set user agent", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Set additional headers to appear more human-like
	headers := map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9",
		"Accept-Encoding":           "gzip, deflate, br",
		"Cache-Control":             "no-cache",
		"Pragma":                    "no-cache",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}

	for name, value := range headers {
		_, err := page.SetExtraHeaders([]string{name, value})
		if err != nil {
			bm.logger.Debug("Failed to set header", map[string]interface{}{
				"error":  err.Error(),
				"header": name,
			})
		}
	}

	// Inject additional stealth JavaScript to mask automation
	err = rod.Try(func() {
		page.MustEval(`() => {
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
			
			// Randomize screen properties slightly
			Object.defineProperty(screen, 'width', {
				get: () => 1920,
			});
			Object.defineProperty(screen, 'height', {
				get: () => 1080,
			});
			Object.defineProperty(screen, 'availWidth', {
				get: () => 1920,
			});
			Object.defineProperty(screen, 'availHeight', {
				get: () => 1050,
			});
			
			// Override WebRTC
			let RTCPeerConnection = window.RTCPeerConnection || window.mozRTCPeerConnection || window.webkitRTCPeerConnection;
			if (RTCPeerConnection) {
				window.RTCPeerConnection = function() {
					throw new Error('WebRTC is disabled');
				};
			}
		}`)
	})
	if err != nil {
		bm.logger.Warn("Failed to inject stealth JavaScript", map[string]interface{}{
			"error": err.Error(),
		})
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

	bi.manager.logger.Debug("Successfully navigated to URL", map[string]interface{}{
		"url": url,
	})
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

// InjectCaptchaSolution injects the captcha solution into the page and submits it
func (bi *BrowserInstance) InjectCaptchaSolution(solution string) error {
	// Find the reCAPTCHA response element and inject the solution
	js := fmt.Sprintf(`
		// Set the reCAPTCHA response token
		if (window.grecaptcha && typeof window.grecaptcha.getResponse === 'function') {
			// For reCAPTCHA v2
			document.getElementById('g-recaptcha-response').innerHTML = '%s';
			
			// Find and trigger the callback
			let recaptchaElement = document.querySelector('.g-recaptcha');
			if (recaptchaElement) {
				let callback = recaptchaElement.getAttribute('data-callback');
				if (callback && typeof window[callback] === 'function') {
					window[callback]('%s');
				}
			}
		}
		
		// For reCAPTCHA invisible or v3
		let responseElements = document.querySelectorAll('[name="g-recaptcha-response"]');
		for (let element of responseElements) {
			element.value = '%s';
			element.innerHTML = '%s';
		}
		
		// Try to submit the form automatically
		let forms = document.querySelectorAll('form');
		for (let form of forms) {
			if (form.querySelector('.g-recaptcha') || form.querySelector('[name="g-recaptcha-response"]')) {
				form.submit();
				break;
			}
		}
		
		// Also try clicking submit buttons
		let submitButtons = document.querySelectorAll('input[type="submit"], button[type="submit"], button');
		for (let button of submitButtons) {
			if (button.textContent.toLowerCase().includes('submit') || 
				button.textContent.toLowerCase().includes('continue') ||
				button.value && button.value.toLowerCase().includes('submit')) {
				button.click();
				break;
			}
		}
	`, solution, solution, solution, solution)

	err := rod.Try(func() {
		bi.Page.MustEval(js)
	})

	if err != nil {
		return fmt.Errorf("failed to inject captcha solution: %w", err)
	}

	bi.manager.logger.Debug("Captcha solution injected successfully")
	return nil
}

// InjectTurnstileSolution injects the Cloudflare Turnstile solution into the page and submits it
func (bi *BrowserInstance) InjectTurnstileSolution(solution string) error {
	// Find the Turnstile response element and inject the solution
	js := fmt.Sprintf(`
		// Set the Turnstile response token
		if (window.turnstile && typeof window.turnstile.reset === 'function') {
			// For Turnstile widget
			let turnstileElements = document.querySelectorAll('[data-sitekey]');
			for (let element of turnstileElements) {
				if (element.closest('.cf-turnstile') || element.classList.contains('cf-turnstile')) {
					// Set the response value
					let responseInput = element.querySelector('input[name="cf-turnstile-response"]');
					if (responseInput) {
						responseInput.value = '%s';
					} else {
						// Create response input if it doesn't exist
						responseInput = document.createElement('input');
						responseInput.type = 'hidden';
						responseInput.name = 'cf-turnstile-response';
						responseInput.value = '%s';
						element.appendChild(responseInput);
					}
					
					// Trigger the callback if available
					let callback = element.getAttribute('data-callback');
					if (callback && typeof window[callback] === 'function') {
						window[callback]('%s');
					}
					break;
				}
			}
		}
		
		// Also check for any hidden inputs with turnstile-related names
		let responseElements = document.querySelectorAll('input[name*="turnstile"], input[name*="cf-turnstile"]');
		for (let element of responseElements) {
			element.value = '%s';
		}
		
		// Try to submit the form automatically
		let forms = document.querySelectorAll('form');
		for (let form of forms) {
			if (form.querySelector('.cf-turnstile') || form.querySelector('[data-sitekey]') || form.querySelector('input[name*="turnstile"]')) {
				form.submit();
				break;
			}
		}
		
		// Also try clicking submit buttons
		let submitButtons = document.querySelectorAll('input[type="submit"], button[type="submit"], button');
		for (let button of submitButtons) {
			if (button.textContent.toLowerCase().includes('submit') || 
				button.textContent.toLowerCase().includes('continue') ||
				button.textContent.toLowerCase().includes('verify') ||
				button.value && button.value.toLowerCase().includes('submit')) {
				button.click();
				break;
			}
		}
	`, solution, solution, solution, solution)

	err := rod.Try(func() {
		bi.Page.MustEval(js)
	})

	if err != nil {
		return fmt.Errorf("failed to inject Turnstile solution: %w", err)
	}

	bi.manager.logger.Debug("Turnstile solution injected successfully")
	return nil
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

// SimulateHumanBehavior simulates human-like behavior to help resolve Cloudflare challenges
func (bi *BrowserInstance) SimulateHumanBehavior() error {
	// Simulate mouse movements and scrolling to appear more human-like
	err := rod.Try(func() {
		// Get page dimensions
		viewport := bi.Page.MustEval(`() => ({
			width: window.innerWidth,
			height: window.innerHeight
		})`)

		width := int(viewport.Get("width").Num())
		height := int(viewport.Get("height").Num())

		// Simulate more natural mouse movements with curves
		for i := 0; i < 5; i++ {
			// Create more random movement patterns
			startX := 100 + (i * 50) + (i % 3 * 100)
			startY := 100 + (i * 30) + (i % 2 * 150)
			endX := startX + 50 + (i * 20)
			endY := startY + 30 + (i * 25)

			if startX < width && startY < height && endX < width && endY < height {
				// Move to start position
				bi.Page.Mouse.MustMoveTo(float64(startX), float64(startY))
				time.Sleep(time.Duration(200+i*100) * time.Millisecond)

				// Curved movement to end position
				midX := (startX + endX) / 2
				midY := (startY + endY) / 2
				bi.Page.Mouse.MustMoveTo(float64(midX), float64(midY))
				time.Sleep(time.Duration(100+i*50) * time.Millisecond)
				bi.Page.Mouse.MustMoveTo(float64(endX), float64(endY))
				time.Sleep(time.Duration(300+i*100) * time.Millisecond)
			}
		}

		// Simulate keyboard activity (focus on body)
		bi.Page.MustEval(`() => {
			document.body.focus();
			// Simulate some key events
			const events = ['keydown', 'keyup'];
			events.forEach(event => {
				document.dispatchEvent(new KeyboardEvent(event, {key: 'Tab'}));
			});
		}`)
		time.Sleep(500 * time.Millisecond)

		// Simulate varied scrolling patterns
		bi.Page.MustEval(`() => {
			// Natural scroll pattern
			window.scrollTo({top: 200, behavior: 'smooth'});
			setTimeout(() => {
				window.scrollTo({top: 50, behavior: 'smooth'});
			}, 800);
			setTimeout(() => {
				window.scrollTo({top: 0, behavior: 'smooth'});
			}, 1600);
		}`)

		// Wait for scrolling to complete
		time.Sleep(2 * time.Second)

		// Simulate some window/document events
		bi.Page.MustEval(`() => {
			// Trigger focus/blur events
			window.dispatchEvent(new Event('focus'));
			setTimeout(() => {
				window.dispatchEvent(new Event('blur'));
			}, 200);
			setTimeout(() => {
				window.dispatchEvent(new Event('focus'));
			}, 400);
			
			// Simulate visibility change
			document.dispatchEvent(new Event('visibilitychange'));
		}`)

		// Additional wait to let any JavaScript challenges complete
		time.Sleep(3 * time.Second)
	})

	if err != nil {
		return fmt.Errorf("failed to simulate human behavior: %w", err)
	}

	bi.manager.logger.Debug("Enhanced human behavior simulation completed")
	return nil
}

// getSystemChromePath finds the system-installed Chrome/Chromium browser
func getSystemChromePath() string {
	// First check environment variables (Docker container configuration)
	if chromeBin := os.Getenv("CHROME_BIN"); chromeBin != "" {
		if _, err := os.Stat(chromeBin); err == nil {
			return chromeBin
		}
	}

	if chromePath := os.Getenv("CHROME_PATH"); chromePath != "" {
		if _, err := os.Stat(chromePath); err == nil {
			return chromePath
		}
	}

	// Check common Chrome/Chromium paths
	commonPaths := []string{
		"/usr/bin/chromium-browser",                                        // Alpine Linux (Docker)
		"/usr/bin/chromium",                                                // Some Linux distros
		"/usr/bin/google-chrome",                                           // Google Chrome on Linux
		"/usr/bin/google-chrome-stable",                                    // Google Chrome stable
		"/opt/google/chrome/chrome",                                        // Alternative Chrome path
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",     // macOS
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",       // Windows
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe", // Windows 32-bit
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
