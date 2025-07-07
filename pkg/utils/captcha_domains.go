package utils

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// CaptchaDomainsFile path can be configured via environment variable
	CaptchaDomainsFile = getConfiguredCaptchaDomainsFile()
)

func getConfiguredCaptchaDomainsFile() string {
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		return fmt.Sprintf("%s/captcha-domains.txt", dataDir)
	}
	return "captcha-domains.txt"
}

// CaptchaDomainManager manages a list of domains known to have captcha protection
type CaptchaDomainManager struct {
	domains map[string]time.Time // domain -> first seen time
	mu      sync.RWMutex
	logger  *logrus.Logger
}

// NewCaptchaDomainManager creates a new captcha domain manager
func NewCaptchaDomainManager() *CaptchaDomainManager {
	manager := &CaptchaDomainManager{
		domains: make(map[string]time.Time),
		logger:  GetLogger(),
	}

	// Load existing domains from file
	if err := manager.loadDomains(); err != nil {
		manager.logger.WithError(err).Warn("Failed to load captcha domains file")
	}

	return manager
}

// IsKnownCaptchaDomain checks if a domain is known to have captcha protection
func (cdm *CaptchaDomainManager) IsKnownCaptchaDomain(urlStr string) bool {
	domain, err := extractDomain(urlStr)
	if err != nil {
		cdm.logger.WithError(err).WithField("url", urlStr).Debug("Failed to extract domain")
		return false
	}

	cdm.mu.RLock()
	defer cdm.mu.RUnlock()

	_, exists := cdm.domains[domain]
	return exists
}

// AddCaptchaDomain adds a domain to the captcha domains list
func (cdm *CaptchaDomainManager) AddCaptchaDomain(urlStr string) error {
	domain, err := extractDomain(urlStr)
	if err != nil {
		return fmt.Errorf("failed to extract domain from URL %s: %w", urlStr, err)
	}

	cdm.mu.Lock()
	defer cdm.mu.Unlock()

	// Check if domain already exists
	if _, exists := cdm.domains[domain]; exists {
		cdm.logger.WithField("domain", domain).Debug("Domain already in captcha list")
		return nil
	}

	// Add domain with current timestamp
	cdm.domains[domain] = time.Now()

	cdm.logger.WithFields(logrus.Fields{
		"domain": domain,
		"url":    urlStr,
	}).Info("Added domain to captcha protection list")

	// Save to file
	if err := cdm.saveDomains(); err != nil {
		return fmt.Errorf("failed to save captcha domains: %w", err)
	}

	return nil
}

// GetKnownDomains returns a copy of all known captcha domains
func (cdm *CaptchaDomainManager) GetKnownDomains() map[string]time.Time {
	cdm.mu.RLock()
	defer cdm.mu.RUnlock()

	result := make(map[string]time.Time)
	for domain, firstSeen := range cdm.domains {
		result[domain] = firstSeen
	}

	return result
}

// GetDomainsCount returns the number of known captcha domains
func (cdm *CaptchaDomainManager) GetDomainsCount() int {
	cdm.mu.RLock()
	defer cdm.mu.RUnlock()
	return len(cdm.domains)
}

// loadDomains loads domains from the captcha domains file
func (cdm *CaptchaDomainManager) loadDomains() error {
	file, err := os.Open(CaptchaDomainsFile)
	if err != nil {
		if os.IsNotExist(err) {
			cdm.logger.Debug("Captcha domains file does not exist, starting with empty list")
			return nil
		}
		return fmt.Errorf("failed to open captcha domains file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	domainsLoaded := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		parts := strings.SplitN(line, "\t", 2)
		domain := parts[0]

		var firstSeen time.Time
		if len(parts) > 1 {
			if parsed, err := time.Parse(time.RFC3339, parts[1]); err == nil {
				firstSeen = parsed
			} else {
				firstSeen = time.Now()
			}
		} else {
			firstSeen = time.Now()
		}

		cdm.domains[domain] = firstSeen
		domainsLoaded++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading captcha domains file: %w", err)
	}

	cdm.logger.WithField("count", domainsLoaded).Info("Loaded captcha domains from file")
	return nil
}

// saveDomains saves the current domains to the captcha domains file
func (cdm *CaptchaDomainManager) saveDomains() error {
	file, err := os.Create(CaptchaDomainsFile)
	if err != nil {
		return fmt.Errorf("failed to create captcha domains file: %w", err)
	}
	defer file.Close()

	// Write header comment
	fmt.Fprintf(file, "# Captcha-protected domains (automatically managed)\n")
	fmt.Fprintf(file, "# Format: domain\\tfirst_seen_timestamp\n")
	fmt.Fprintf(file, "# This file is auto-generated and should not be manually edited\n\n")

	// Write domains
	for domain, firstSeen := range cdm.domains {
		fmt.Fprintf(file, "%s\t%s\n", domain, firstSeen.Format(time.RFC3339))
	}

	return nil
}

// extractDomain extracts the domain from a URL string
func extractDomain(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("no hostname found in URL")
	}

	// Remove www. prefix if present
	if strings.HasPrefix(hostname, "www.") {
		hostname = hostname[4:]
	}

	return hostname, nil
}
