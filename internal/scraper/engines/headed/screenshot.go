package headed

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// ScreenshotService handles resume screenshot generation using global browser pool
type ScreenshotService struct {
	config *config.Config
	logger types.Logger
}

// NewScreenshotService creates a new screenshot service that uses the global browser pool
func NewScreenshotService(cfg *config.Config) *ScreenshotService {
	logger := logging.GetGlobalLogger()

	return &ScreenshotService{
		config: cfg,
		logger: logger,
	}
}

// CaptureResumeScreenshot captures a screenshot of a resume from letraz-client
func (ss *ScreenshotService) CaptureResumeScreenshot(ctx context.Context, resumeID string) ([]byte, error) {
	ss.logger.Info("Starting resume screenshot capture", map[string]interface{}{
		"resume_id": resumeID,
	})

	// Create a timeout context for the entire screenshot operation
	screenshotCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Get global browser pool
	globalPool, err := GetGlobalBrowserPool()
	if err != nil {
		ss.logger.Error("Failed to get global browser pool", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
		return nil, fmt.Errorf("failed to get global browser pool: %w", err)
	}

	// Acquire a browser instance from the global pool
	browserInstance, err := globalPool.AcquireBrowser(screenshotCtx)
	if err != nil {
		ss.logger.Error("Failed to acquire browser instance for screenshot", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
		return nil, fmt.Errorf("failed to acquire browser instance: %w", err)
	}
	defer browserInstance.Release()

	// Construct the URL for the resume preview with proper escaping
	escapedID := url.PathEscape(resumeID)
	escapedToken := url.QueryEscape(ss.config.Resume.Client.PreviewToken)
	previewURL := fmt.Sprintf("%s/%s?token=%s",
		strings.TrimRight(ss.config.Resume.Client.PreviewURL, "/"),
		escapedID,
		escapedToken,
	)

	ss.logger.Info("Navigating to resume preview URL", map[string]interface{}{
		"resume_id":   resumeID,
		"preview_url": previewURL,
	})

	// Navigate to the resume preview page with timeout
	navigationCtx, navCancel := context.WithTimeout(screenshotCtx, 30*time.Second)
	defer navCancel()

	err = browserInstance.Page.Context(navigationCtx).Navigate(previewURL)
	if err != nil {
		ss.logger.Error("Failed to navigate to resume preview page", map[string]interface{}{
			"resume_id":   resumeID,
			"preview_url": previewURL,
			"error":       err.Error(),
		})
		return nil, fmt.Errorf("failed to navigate to resume preview: %w", err)
	}

	// Wait for the page to load completely with timeout
	loadCtx, loadCancel := context.WithTimeout(screenshotCtx, 20*time.Second)
	defer loadCancel()

	err = browserInstance.Page.Context(loadCtx).WaitLoad()
	if err != nil {
		ss.logger.Error("Failed to wait for page load", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
		return nil, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Set A4 viewport for proper resume rendering (with dedicated timeout)
	viewportCtx, viewportCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer viewportCancel()

	err = browserInstance.Page.Context(viewportCtx).SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             794,  // A4 width at 96 DPI (210mm)
		Height:            1123, // A4 height at 96 DPI (297mm)
		DeviceScaleFactor: 1,    // Standard DPI for faster rendering
		Mobile:            false,
	})
	if err != nil {
		ss.logger.Warn("Failed to set viewport, using default", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
	}

	// Wait for content to load efficiently
	time.Sleep(1 * time.Second)

	// Wait for any specific elements that indicate the resume is fully loaded
	err = ss.waitForResumeToLoad(screenshotCtx, browserInstance.Page)
	if err != nil {
		ss.logger.Warn("Resume loading check failed, proceeding with screenshot", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
	}

	ss.logger.Info("Page setup complete, waiting for final render", map[string]interface{}{
		"resume_id": resumeID,
	})

	// Capture the full page screenshot with high quality and timeout
	ss.logger.Info("Capturing high-quality full-page screenshot", map[string]interface{}{
		"resume_id": resumeID,
	})

	captureCtx, captureCancel := context.WithTimeout(screenshotCtx, 30*time.Second)
	defer captureCancel()

	quality := int(90) // Good quality balance between file size and rendering speed
	screenshot, err := browserInstance.Page.Context(captureCtx).Screenshot(true, &proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: &quality, // Balanced quality for professional resumes
	})

	if err != nil {
		ss.logger.Error("Failed to capture screenshot", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	ss.logger.Info("Screenshot captured successfully", map[string]interface{}{
		"resume_id":  resumeID,
		"size_bytes": len(screenshot),
	})

	return screenshot, nil
}

// waitForResumeToLoad waits for specific elements that indicate the resume is fully loaded
func (ss *ScreenshotService) waitForResumeToLoad(ctx context.Context, page *rod.Page) error {
	// Wait for common resume elements with shorter timeouts
	selectors := []string{
		".resume-container",
		".resume-content",
		"[data-testid='resume']",
		".resume",
		"main",
		"article",
		"body",
	}

	// Try each selector with a shorter timeout
	for _, selector := range selectors {
		selectorCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := page.Context(selectorCtx).Element(selector)
		cancel()

		if err == nil {
			ss.logger.Info("Found resume element, waiting for content to stabilize", map[string]interface{}{
				"selector": selector,
			})

			// Wait for network idle efficiently with timeout
			idleCtx, idleCancel := context.WithTimeout(ctx, 3*time.Second)
			page.Context(idleCtx).WaitIdle(1 * time.Second)
			idleCancel()

			// Brief wait for rendering stability
			time.Sleep(500 * time.Millisecond)
			return nil
		}
	}

	// Fallback: minimal wait for basic rendering
	ss.logger.Warn("No specific resume elements found, using minimal wait", map[string]interface{}{})

	idleCtx, idleCancel := context.WithTimeout(ctx, 2*time.Second)
	page.Context(idleCtx).WaitIdle(1 * time.Second)
	idleCancel()

	time.Sleep(500 * time.Millisecond)

	return nil // Don't fail if we can't find specific elements
}

// IsHealthy checks if the screenshot service is healthy
func (ss *ScreenshotService) IsHealthy() bool {
	// Check if global browser pool is available and healthy
	globalPool, err := GetGlobalBrowserPool()
	if err != nil {
		ss.logger.Error("Screenshot service health check failed - no global browser pool", map[string]interface{}{
			"error": err.Error(),
		})
		return false
	}

	return globalPool.IsHealthy()
}

// Cleanup is a no-op for the new screenshot service since it uses global browser pool
// The global browser pool manages its own cleanup
func (ss *ScreenshotService) Cleanup() {
	ss.logger.Info("Screenshot service cleanup - using global browser pool, no local cleanup needed")
}
