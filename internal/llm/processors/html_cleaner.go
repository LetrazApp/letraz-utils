package processors

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// HTMLCleaner provides functionality to clean and preprocess HTML content
type HTMLCleaner struct {
	// Tags to remove completely
	removeTags []string
	// Attributes to keep (others will be removed)
	keepAttributes []string
}

// NewHTMLCleaner creates a new HTML cleaner instance
func NewHTMLCleaner() *HTMLCleaner {
	return &HTMLCleaner{
		removeTags: []string{
			"script", "style", "noscript", "iframe", "object", "embed",
			"applet", "form", "input", "button", "select", "textarea",
			"nav", "header", "footer", "aside", "menu", "menuitem",
			"svg", "path", "g", "defs", "use", "symbol",
			"meta", "link", "title", "base",
		},
		keepAttributes: []string{
			"class", "id", "data-testid", "data-test", "aria-label", "title",
		},
	}
}

// CleanHTML removes unnecessary elements and clutter from HTML
func (hc *HTMLCleaner) CleanHTML(html string) (string, error) {
	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// Remove unwanted tags
	for _, tag := range hc.removeTags {
		doc.Find(tag).Remove()
	}

	// Remove comments
	hc.removeComments(doc)

	// Clean attributes
	hc.cleanAttributes(doc)

	// Remove empty elements
	hc.removeEmptyElements(doc)

	// Get cleaned HTML
	cleanedHTML, err := doc.Html()
	if err != nil {
		return "", err
	}

	// Additional text cleaning
	cleanedHTML = hc.cleanText(cleanedHTML)

	return cleanedHTML, nil
}

// ExtractJobContent specifically extracts content likely to contain job information
func (hc *HTMLCleaner) ExtractJobContent(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// Job-specific selectors (common patterns for job postings)
	jobSelectors := []string{
		// Main content areas
		"main", "[role='main']", "#main", ".main",
		// Job-specific containers
		".job", ".job-posting", ".job-detail", ".job-description",
		".posting", ".position", ".vacancy", ".opportunity",
		// Content areas
		".content", ".description", ".details", ".info",
		// Article/section containers
		"article", "section[class*='job']", "section[class*='posting']",
		// Specific data attributes
		"[data-testid*='job']", "[data-test*='job']", "[data-qa*='job']",
	}

	var contentParts []string

	// Try to find job-specific content
	for _, selector := range jobSelectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); text != "" && len(text) > 50 {
				contentParts = append(contentParts, text)
			}
		})
	}

	// If no specific job content found, fall back to body content
	if len(contentParts) == 0 {
		if bodyText := doc.Find("body").Text(); bodyText != "" {
			contentParts = append(contentParts, bodyText)
		}
	}

	// Combine and clean the content
	combinedContent := strings.Join(contentParts, "\n\n")

	// Clean up whitespace and formatting
	cleanedContent := hc.cleanExtractedText(combinedContent)

	return cleanedContent, nil
}

// removeComments removes HTML comments
func (hc *HTMLCleaner) removeComments(doc *goquery.Document) {
	// goquery doesn't handle comments directly, so we'll use regex after HTML generation
}

// cleanAttributes removes unwanted attributes from elements
func (hc *HTMLCleaner) cleanAttributes(doc *goquery.Document) {
	doc.Find("*").Each(func(i int, s *goquery.Selection) {
		// Get all attributes
		for _, attr := range s.Nodes[0].Attr {
			keep := false
			for _, keepAttr := range hc.keepAttributes {
				if attr.Key == keepAttr {
					keep = true
					break
				}
			}

			if !keep {
				s.RemoveAttr(attr.Key)
			}
		}
	})
}

// removeEmptyElements removes elements that are empty or contain only whitespace
func (hc *HTMLCleaner) removeEmptyElements(doc *goquery.Document) {
	// Remove elements that are empty or contain only whitespace
	doc.Find("*").Each(func(i int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == "" && s.Children().Length() == 0 {
			s.Remove()
		}
	})
}

// cleanText performs additional text cleaning
func (hc *HTMLCleaner) cleanText(html string) string {
	// Remove HTML comments
	commentRegex := regexp.MustCompile(`<!--[\s\S]*?-->`)
	html = commentRegex.ReplaceAllString(html, "")

	// Remove excessive whitespace
	whitespaceRegex := regexp.MustCompile(`\s+`)
	html = whitespaceRegex.ReplaceAllString(html, " ")

	// Remove empty lines
	emptyLineRegex := regexp.MustCompile(`\n\s*\n`)
	html = emptyLineRegex.ReplaceAllString(html, "\n")

	return strings.TrimSpace(html)
}

// cleanExtractedText cleans extracted text content
func (hc *HTMLCleaner) cleanExtractedText(text string) string {
	// Remove excessive whitespace
	whitespaceRegex := regexp.MustCompile(`\s+`)
	text = whitespaceRegex.ReplaceAllString(text, " ")

	// Remove excessive newlines
	newlineRegex := regexp.MustCompile(`\n{3,}`)
	text = newlineRegex.ReplaceAllString(text, "\n\n")

	// Clean up common unwanted patterns
	patterns := []string{
		`\bJavaScript\s+is\s+disabled\b.*?enabled\.`,
		`\bCookies?\s+are\s+disabled\b.*?enabled\.`,
		`\bPlease\s+enable\s+JavaScript\b.*?`,
		`\bThis\s+site\s+requires\s+JavaScript\b.*?`,
	}

	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		text = regex.ReplaceAllString(text, "")
	}

	return strings.TrimSpace(text)
}

// GetCleanTextLength returns the approximate token count for the cleaned text
func (hc *HTMLCleaner) GetCleanTextLength(text string) int {
	// Rough estimation: ~4 characters per token
	return len(text) / 4
}
