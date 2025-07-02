package headed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/sirupsen/logrus"
	"letraz-scrapper/internal/config"
	"letraz-scrapper/internal/llm"
	"letraz-scrapper/internal/scraper/captcha"
	"letraz-scrapper/pkg/models"
	"letraz-scrapper/pkg/utils"
)

// RodScraper implements job scraping using Rod browser automation
type RodScraper struct {
	config         *config.Config
	browserManager *BrowserManager
	llmManager     *llm.Manager
	captchaSolver  captcha.CaptchaSolver
	logger         *logrus.Logger
}

// ScrapingResult represents the result of a scraping operation
type ScrapingResult struct {
	HTML        string
	Title       string
	URL         string
	Success     bool
	Error       error
	ProcessTime time.Duration
}

// NewRodScraper creates a new Rod scraper instance
func NewRodScraper(cfg *config.Config, llmManager *llm.Manager) *RodScraper {
	return &RodScraper{
		config:         cfg,
		browserManager: NewBrowserManager(cfg),
		llmManager:     llmManager,
		captchaSolver:  captcha.NewTwoCaptchaSolver(cfg),
		logger:         utils.GetLogger(),
	}
}

// ScrapeJob scrapes a job posting from the given URL using LLM processing
func (rs *RodScraper) ScrapeJob(ctx context.Context, url string, options *models.ScrapeOptions) (*models.Job, error) {
	startTime := time.Now()

	rs.logger.WithFields(logrus.Fields{
		"url":    url,
		"engine": "rod_llm",
	}).Info("Starting job scrape with Rod engine and LLM processing")

	// Get browser instance
	browser, err := rs.browserManager.GetBrowser(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get browser instance: %w", err)
	}
	defer browser.Release()

	// Set timeout from options or config
	timeout := rs.config.Scraper.RequestTimeout
	if options != nil && options.Timeout > 0 {
		timeout = options.Timeout
	}

	// Navigate to the URL
	err = browser.Navigate(ctx, url, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to URL: %w", err)
	}

	// Wait for page to be fully loaded
	time.Sleep(2 * time.Second)

	// Get initial page HTML to check for captcha
	initialHTML, err := browser.GetPageHTML()
	if err != nil {
		return nil, fmt.Errorf("failed to get initial page HTML: %w", err)
	}

	// Check for captcha and solve if present
	hasCaptcha, siteKey, err := captcha.DetectCaptcha(initialHTML)
	if err != nil {
		rs.logger.WithField("url", url).Debug("Error detecting captcha, continuing with scraping")
	} else if hasCaptcha {
		rs.logger.WithFields(logrus.Fields{
			"url":      url,
			"site_key": siteKey,
		}).Info("Captcha detected, attempting to solve")

		if strings.HasPrefix(siteKey, "turnstile:") {
			// Handle Cloudflare Turnstile
			actualSiteKey := strings.TrimPrefix(siteKey, "turnstile:")
			rs.logger.WithFields(logrus.Fields{
				"url":      url,
				"site_key": actualSiteKey,
			}).Info("Cloudflare Turnstile detected, solving with 2CAPTCHA")

			solution, solveErr := rs.captchaSolver.SolveTurnstile(ctx, actualSiteKey, url)
			if solveErr != nil {
				rs.logger.WithFields(logrus.Fields{
					"url":      url,
					"site_key": actualSiteKey,
					"error":    solveErr.Error(),
				}).Error("Failed to solve Cloudflare Turnstile")
				return nil, fmt.Errorf("failed to solve Cloudflare Turnstile: %w", solveErr)
			} else {
				// Inject the Turnstile solution into the page
				injectErr := browser.InjectTurnstileSolution(solution)
				if injectErr != nil {
					rs.logger.WithFields(logrus.Fields{
						"url":   url,
						"error": injectErr.Error(),
					}).Warn("Failed to inject Turnstile solution")
				} else {
					rs.logger.WithField("url", url).Info("Turnstile solved and injected successfully")

					// Wait for page to reload after solution submission
					time.Sleep(3 * time.Second)

					// Get the new page content
					newHTML, newHTMLErr := browser.GetPageHTML()
					if newHTMLErr == nil {
						initialHTML = newHTML
					}
				}
			}
		} else if siteKey == "cloudflare" {
			// Generic Cloudflare challenge - try to extract actual site key first
			rs.logger.WithField("url", url).Info("Generic Cloudflare challenge detected, trying to extract actual site key")

			// Check if 2CAPTCHA service is available
			if rs.captchaSolver == nil || !rs.captchaSolver.IsHealthy() {
				rs.logger.WithFields(logrus.Fields{
					"url":            url,
					"captcha_solver": rs.captchaSolver != nil,
					"solver_healthy": rs.captchaSolver != nil && rs.captchaSolver.IsHealthy(),
				}).Error("2CAPTCHA service not available for Cloudflare challenge")
				return nil, fmt.Errorf("Cloudflare captcha detected but 2CAPTCHA service is not available for URL: %s", url)
			}

			rs.logger.WithField("url", url).Info("2CAPTCHA service is healthy, attempting Cloudflare bypass")

			// Try to extract actual Turnstile site key from the page
			actualSiteKey := extractTurnstileSiteKeyFromHTML(initialHTML)
			if actualSiteKey == "" {
				rs.logger.WithFields(logrus.Fields{
					"url":         url,
					"html_length": len(initialHTML),
					"html_sample": initialHTML[:min(500, len(initialHTML))], // Log first 500 chars for debugging
				}).Info("No Turnstile site key found initially, simulating human behavior to trigger challenge progression")

				// Simulate human behavior to help Cloudflare challenge progress
				if err := browser.SimulateHumanBehavior(); err != nil {
					rs.logger.WithFields(logrus.Fields{
						"url":   url,
						"error": err.Error(),
					}).Warn("Failed to simulate human behavior")
				}

				// Wait additional time for Cloudflare iframe to be loaded after human simulation
				time.Sleep(5 * time.Second)

				// Get updated HTML content after iframe loads
				updatedHTML, htmlErr := browser.GetPageHTML()
				if htmlErr == nil {
					actualSiteKey = extractTurnstileSiteKeyFromHTML(updatedHTML)
					if actualSiteKey != "" {
						rs.logger.WithFields(logrus.Fields{
							"url":         url,
							"site_key":    actualSiteKey,
							"html_length": len(updatedHTML),
						}).Info("Found Turnstile site key after waiting for iframe")
						initialHTML = updatedHTML // Update for later use
					} else {
						rs.logger.WithFields(logrus.Fields{
							"url":         url,
							"html_length": len(updatedHTML),
							"html_sample": updatedHTML[:min(500, len(updatedHTML))],
						}).Warn("Still no Turnstile site key found after waiting")
					}
				}

				if actualSiteKey == "" {
					return nil, fmt.Errorf("Cloudflare Turnstile challenge detected but no site key found for URL: %s", url)
				}
			}

			rs.logger.WithFields(logrus.Fields{
				"url":      url,
				"site_key": actualSiteKey,
			}).Info("Found Turnstile site key, attempting 2CAPTCHA solving")

			// Try 2CAPTCHA Turnstile solving with the actual site key
			solution, solveErr := rs.captchaSolver.SolveTurnstile(ctx, actualSiteKey, url)
			if solveErr != nil {
				rs.logger.WithFields(logrus.Fields{
					"url":      url,
					"site_key": actualSiteKey,
					"error":    solveErr.Error(),
				}).Error("2CAPTCHA Turnstile solving failed")
				return nil, fmt.Errorf("failed to solve Cloudflare Turnstile challenge for URL: %s - %w", url, solveErr)
			}

			rs.logger.WithFields(logrus.Fields{
				"url":      url,
				"site_key": actualSiteKey,
			}).Info("2CAPTCHA Turnstile solved successfully")

			// Inject the solution
			injectErr := browser.InjectTurnstileSolution(solution)
			if injectErr != nil {
				rs.logger.WithFields(logrus.Fields{
					"url":   url,
					"error": injectErr.Error(),
				}).Error("Failed to inject Turnstile solution")
				return nil, fmt.Errorf("failed to inject Turnstile solution for URL: %s - %w", url, injectErr)
			}

			rs.logger.WithField("url", url).Info("Turnstile solution injected successfully")

			// Wait for page to reload after solution submission
			time.Sleep(5 * time.Second)

			// Get the new page content
			newHTML, newHTMLErr := browser.GetPageHTML()
			if newHTMLErr == nil && captcha.IsCloudflareResolved(newHTML) {
				rs.logger.WithField("url", url).Info("Cloudflare challenge resolved via 2CAPTCHA")
				initialHTML = newHTML
			} else {
				rs.logger.WithField("url", url).Error("Turnstile solution injection failed to resolve challenge")
				return nil, fmt.Errorf("Cloudflare challenge still not resolved after 2CAPTCHA solution injection for URL: %s", url)
			}
		} else if siteKey != "" && rs.captchaSolver != nil {
			// Handle reCAPTCHA using 2CAPTCHA
			rs.logger.WithFields(logrus.Fields{
				"url":      url,
				"site_key": siteKey,
			}).Info("reCAPTCHA detected, solving with 2CAPTCHA")

			solution, solveErr := rs.captchaSolver.SolveRecaptcha(ctx, siteKey, url)
			if solveErr != nil {
				rs.logger.WithFields(logrus.Fields{
					"url":      url,
					"site_key": siteKey,
					"error":    solveErr.Error(),
				}).Error("Failed to solve reCAPTCHA")
				return nil, fmt.Errorf("failed to solve reCAPTCHA: %w", solveErr)
			} else {
				// Inject the reCAPTCHA solution into the page
				injectErr := browser.InjectCaptchaSolution(solution)
				if injectErr != nil {
					rs.logger.WithFields(logrus.Fields{
						"url":   url,
						"error": injectErr.Error(),
					}).Warn("Failed to inject reCAPTCHA solution")
				} else {
					rs.logger.WithField("url", url).Info("reCAPTCHA solved and injected successfully")

					// Wait for page to reload after captcha submission
					time.Sleep(3 * time.Second)

					// Get the new page content
					newHTML, newHTMLErr := browser.GetPageHTML()
					if newHTMLErr == nil {
						initialHTML = newHTML
					}
				}
			}
		}
	}

	// Use the HTML (either original or post-captcha)
	html := initialHTML

	// Use LLM to extract job information from HTML
	job, err := rs.llmManager.ExtractJobData(ctx, html, url)
	if err != nil {
		// Don't wrap CustomError types so they can be properly handled upstream
		if _, ok := err.(*utils.CustomError); ok {
			return nil, err
		}
		return nil, fmt.Errorf("failed to extract job information using LLM: %w", err)
	}

	processingTime := time.Since(startTime)

	rs.logger.WithFields(logrus.Fields{
		"url":             url,
		"job_title":       job.Title,
		"company":         job.CompanyName,
		"processing_time": processingTime,
		"engine":          "rod_llm",
	}).Info("Job scraping completed successfully with LLM processing")

	return job, nil
}

// ScrapeJobLegacy scrapes a job posting using legacy HTML parsing (for backward compatibility)
func (rs *RodScraper) ScrapeJobLegacy(ctx context.Context, url string, options *models.ScrapeOptions) (*models.JobPosting, error) {
	startTime := time.Now()

	rs.logger.WithFields(logrus.Fields{
		"url":    url,
		"engine": "rod_legacy",
	}).Info("Starting job scrape with Rod engine (legacy mode)")

	// Get browser instance
	browser, err := rs.browserManager.GetBrowser(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get browser instance: %w", err)
	}
	defer browser.Release()

	// Set timeout from options or config
	timeout := rs.config.Scraper.RequestTimeout
	if options != nil && options.Timeout > 0 {
		timeout = options.Timeout
	}

	// Navigate to the URL
	err = browser.Navigate(ctx, url, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to URL: %w", err)
	}

	// Wait for page to be fully loaded
	time.Sleep(2 * time.Second)

	// Get page HTML
	html, err := browser.GetPageHTML()
	if err != nil {
		return nil, fmt.Errorf("failed to get page HTML: %w", err)
	}

	// Extract job information from HTML using legacy method
	jobPosting, err := rs.extractJobFromHTML(html, url)
	if err != nil {
		return nil, fmt.Errorf("failed to extract job information: %w", err)
	}

	processingTime := time.Since(startTime)
	jobPosting.ProcessedAt = time.Now()
	jobPosting.Metadata["processing_time"] = processingTime.String()
	jobPosting.Metadata["engine"] = "rod_legacy"

	rs.logger.WithFields(logrus.Fields{
		"url":             url,
		"job_title":       jobPosting.Title,
		"company":         jobPosting.Company,
		"processing_time": processingTime,
	}).Info("Job scraping completed successfully (legacy mode)")

	return jobPosting, nil
}

// extractJobFromHTML extracts job information from HTML content
func (rs *RodScraper) extractJobFromHTML(html, url string) (*models.JobPosting, error) {
	// Parse HTML with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Generate job ID
	jobID := utils.GenerateRequestID()

	// Initialize job posting
	job := &models.JobPosting{
		ID:             jobID,
		ApplicationURL: url,
		Metadata:       make(map[string]string),
		ProcessedAt:    time.Now(),
	}

	// Extract job title
	job.Title = rs.extractJobTitle(doc)

	// Extract company name
	job.Company = rs.extractCompany(doc)

	// Extract location
	job.Location = rs.extractLocation(doc)

	// Extract description
	job.Description = rs.extractDescription(doc)

	// Extract job type and experience level
	job.JobType = rs.extractJobType(doc)
	job.ExperienceLevel = rs.extractExperienceLevel(doc)

	// Extract requirements and skills
	job.Requirements = rs.extractRequirements(doc)
	job.Skills = rs.extractSkills(doc)

	// Extract benefits
	job.Benefits = rs.extractBenefits(doc)

	// Check if it's remote
	job.Remote = rs.isRemoteJob(doc, job.Location)

	// Extract salary information
	job.Salary = rs.extractSalary(doc)

	// Extract posted date
	job.PostedDate = rs.extractPostedDate(doc)

	// Add metadata
	job.Metadata["html_length"] = fmt.Sprintf("%d", len(html))
	job.Metadata["extraction_method"] = "goquery_selectors"

	return job, nil
}

// extractJobTitle extracts the job title from various common selectors
func (rs *RodScraper) extractJobTitle(doc *goquery.Document) string {
	selectors := []string{
		"h1[data-testid*='job-title'], h1[class*='job-title'], h1[class*='jobTitle']",
		"h1[class*='title']",
		".job-title, .jobTitle, .job_title",
		"h1",
		"[data-testid*='job-title']",
		"title",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			return rs.cleanText(text)
		}
	}

	return "Job Title Not Found"
}

// extractCompany extracts the company name
func (rs *RodScraper) extractCompany(doc *goquery.Document) string {
	selectors := []string{
		"[data-testid*='company'], [class*='company-name'], [class*='companyName']",
		".company, .employer, .organization",
		"[class*='employer']",
		"a[href*='company'], a[href*='employer']",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			return rs.cleanText(text)
		}
	}

	return "Company Not Found"
}

// extractLocation extracts the job location
func (rs *RodScraper) extractLocation(doc *goquery.Document) string {
	selectors := []string{
		"[data-testid*='location'], [class*='location'], [class*='job-location']",
		".location, .address, .city",
		"[class*='city'], [class*='region']",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			return rs.cleanText(text)
		}
	}

	return ""
}

// extractDescription extracts the job description
func (rs *RodScraper) extractDescription(doc *goquery.Document) string {
	selectors := []string{
		"[data-testid*='description'], [class*='job-description'], [class*='jobDescription']",
		".description, .job-content, .content",
		"[class*='summary'], [class*='details']",
		"div[class*='description']",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			return rs.cleanText(text)
		}
	}

	return ""
}

// extractJobType extracts the job type (full-time, part-time, etc.)
func (rs *RodScraper) extractJobType(doc *goquery.Document) string {
	selectors := []string{
		"[data-testid*='job-type'], [class*='job-type'], [class*='employment-type']",
		".job-type, .employment, .type",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			text = rs.cleanText(text)
			if rs.isValidJobType(text) {
				return text
			}
		}
	}

	// Try to infer from description
	description := rs.extractDescription(doc)
	return rs.inferJobTypeFromText(description)
}

// extractExperienceLevel extracts the experience level required
func (rs *RodScraper) extractExperienceLevel(doc *goquery.Document) string {
	selectors := []string{
		"[class*='experience'], [class*='level'], [class*='seniority']",
		".experience, .level, .seniority",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			text = rs.cleanText(text)
			if rs.isValidExperienceLevel(text) {
				return text
			}
		}
	}

	// Try to infer from description
	description := rs.extractDescription(doc)
	return rs.inferExperienceLevelFromText(description)
}

// extractRequirements extracts job requirements as a list
func (rs *RodScraper) extractRequirements(doc *goquery.Document) []string {
	var requirements []string

	selectors := []string{
		"[class*='requirement'], [class*='qualification']",
		".requirements li, .qualifications li",
		"ul li, ol li",
	}

	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); text != "" && len(text) > 10 {
				requirements = append(requirements, rs.cleanText(text))
			}
		})
		if len(requirements) > 0 {
			break
		}
	}

	return rs.deduplicateStrings(requirements)
}

// extractSkills extracts required skills
func (rs *RodScraper) extractSkills(doc *goquery.Document) []string {
	var skills []string

	// Look for skills sections
	selectors := []string{
		"[class*='skill'], [class*='technology'], [class*='tech-stack']",
		".skills li, .technologies li",
	}

	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); text != "" && len(text) < 50 {
				skills = append(skills, rs.cleanText(text))
			}
		})
	}

	// Extract common tech skills from description
	description := rs.extractDescription(doc)
	extractedSkills := rs.extractSkillsFromText(description)
	skills = append(skills, extractedSkills...)

	return rs.deduplicateStrings(skills)
}

// extractBenefits extracts job benefits
func (rs *RodScraper) extractBenefits(doc *goquery.Document) []string {
	var benefits []string

	selectors := []string{
		"[class*='benefit'], [class*='perk']",
		".benefits li, .perks li",
	}

	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); text != "" {
				benefits = append(benefits, rs.cleanText(text))
			}
		})
	}

	return rs.deduplicateStrings(benefits)
}

// isRemoteJob determines if the job is remote based on location and content
func (rs *RodScraper) isRemoteJob(doc *goquery.Document, location string) bool {
	// Check location for remote keywords
	locationLower := strings.ToLower(location)
	remoteKeywords := []string{"remote", "anywhere", "home", "distributed", "virtual"}

	for _, keyword := range remoteKeywords {
		if strings.Contains(locationLower, keyword) {
			return true
		}
	}

	// Check full page content for remote mentions
	pageText := strings.ToLower(doc.Text())
	remoteIndicators := []string{"work from home", "remote work", "fully remote", "100% remote"}

	for _, indicator := range remoteIndicators {
		if strings.Contains(pageText, indicator) {
			return true
		}
	}

	return false
}

// extractSalary extracts salary information
func (rs *RodScraper) extractSalary(doc *goquery.Document) *models.SalaryRange {
	selectors := []string{
		"[class*='salary'], [class*='pay'], [class*='compensation']",
		".salary, .pay, .wage, .compensation",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			return rs.parseSalaryFromText(text)
		}
	}

	return nil
}

// extractPostedDate extracts when the job was posted
func (rs *RodScraper) extractPostedDate(doc *goquery.Document) time.Time {
	selectors := []string{
		"[class*='posted'], [class*='date'], time",
		".posted, .date, .timestamp",
	}

	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).First().Text()); text != "" {
			if date := rs.parseDateFromText(text); !date.IsZero() {
				return date
			}
		}
	}

	return time.Now() // Default to current time if not found
}

// Helper methods for text processing and validation

func (rs *RodScraper) cleanText(text string) string {
	// Remove extra whitespace and clean up text
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	// Remove multiple spaces
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}

	return text
}

func (rs *RodScraper) isValidJobType(text string) bool {
	validTypes := []string{"full-time", "part-time", "contract", "freelance", "temporary", "internship"}
	textLower := strings.ToLower(text)

	for _, validType := range validTypes {
		if strings.Contains(textLower, validType) {
			return true
		}
	}
	return false
}

func (rs *RodScraper) isValidExperienceLevel(text string) bool {
	validLevels := []string{"entry", "junior", "mid", "senior", "lead", "principal", "director", "manager"}
	textLower := strings.ToLower(text)

	for _, level := range validLevels {
		if strings.Contains(textLower, level) {
			return true
		}
	}
	return false
}

func (rs *RodScraper) inferJobTypeFromText(text string) string {
	textLower := strings.ToLower(text)

	if strings.Contains(textLower, "full-time") || strings.Contains(textLower, "full time") {
		return "Full-time"
	}
	if strings.Contains(textLower, "part-time") || strings.Contains(textLower, "part time") {
		return "Part-time"
	}
	if strings.Contains(textLower, "contract") {
		return "Contract"
	}
	if strings.Contains(textLower, "internship") || strings.Contains(textLower, "intern") {
		return "Internship"
	}

	return "Full-time" // Default assumption
}

func (rs *RodScraper) inferExperienceLevelFromText(text string) string {
	textLower := strings.ToLower(text)

	if strings.Contains(textLower, "senior") || strings.Contains(textLower, "sr.") {
		return "Senior"
	}
	if strings.Contains(textLower, "junior") || strings.Contains(textLower, "jr.") {
		return "Junior"
	}
	if strings.Contains(textLower, "lead") || strings.Contains(textLower, "principal") {
		return "Lead"
	}
	if strings.Contains(textLower, "entry") || strings.Contains(textLower, "graduate") {
		return "Entry-level"
	}

	return "Mid-level" // Default assumption
}

func (rs *RodScraper) extractSkillsFromText(text string) []string {
	// Common programming languages and technologies
	commonSkills := []string{
		"JavaScript", "Python", "Java", "Go", "Golang", "React", "Node.js", "TypeScript",
		"Docker", "Kubernetes", "AWS", "Azure", "GCP", "PostgreSQL", "MySQL", "MongoDB",
		"Redis", "Git", "Linux", "HTML", "CSS", "SQL", "NoSQL", "REST", "GraphQL",
		"Microservices", "DevOps", "CI/CD", "Terraform", "Jenkins", "Nginx",
	}

	var foundSkills []string
	textLower := strings.ToLower(text)

	for _, skill := range commonSkills {
		if strings.Contains(textLower, strings.ToLower(skill)) {
			foundSkills = append(foundSkills, skill)
		}
	}

	return foundSkills
}

func (rs *RodScraper) parseSalaryFromText(text string) *models.SalaryRange {
	// This is a basic implementation - could be enhanced with more sophisticated parsing
	// For now, return nil to indicate salary parsing is not implemented
	return nil
}

func (rs *RodScraper) parseDateFromText(text string) time.Time {
	// Basic date parsing - could be enhanced
	// For now, return zero time to indicate date parsing is not implemented
	return time.Time{}
}

func (rs *RodScraper) deduplicateStrings(slice []string) []string {
	keys := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !keys[item] && item != "" {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}

// Cleanup releases all browser resources
func (rs *RodScraper) Cleanup() {
	rs.browserManager.Cleanup()
}

// IsHealthy checks if the scraper is healthy
func (rs *RodScraper) IsHealthy() bool {
	return rs.browserManager.IsHealthy()
}

// extractTurnstileSiteKeyFromHTML extracts the Cloudflare Turnstile site key from HTML content
func extractTurnstileSiteKeyFromHTML(html string) string {
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
