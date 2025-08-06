package utils

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// LinkedInURLType represents the type of LinkedIn URL
type LinkedInURLType int

const (
	LinkedInURLTypeUnknown       LinkedInURLType = iota
	LinkedInURLTypeJobView                       // Direct job view: /jobs/view/123
	LinkedInURLTypeJobCollection                 // Job collection: /jobs/collections/recommended/?currentJobId=123
	LinkedInURLTypeNonJob                        // Non-job URLs: profiles, company pages, etc.
)

// LinkedInURLInfo contains information about a parsed LinkedIn URL
type LinkedInURLInfo struct {
	Type      LinkedInURLType
	JobID     string
	PublicURL string
}

// IsLinkedInURL checks if a URL is a LinkedIn URL
func IsLinkedInURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	hostname := strings.ToLower(parsedURL.Hostname())
	return hostname == "linkedin.com" || hostname == "www.linkedin.com"
}

// ParseLinkedInURL analyzes a LinkedIn URL and returns information about its type and job ID
func ParseLinkedInURL(urlStr string) (*LinkedInURLInfo, error) {
	if !IsLinkedInURL(urlStr) {
		return nil, fmt.Errorf("not a LinkedIn URL: %s", urlStr)
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	path := strings.ToLower(parsedURL.Path)
	query := parsedURL.Query()

	info := &LinkedInURLInfo{
		Type: LinkedInURLTypeUnknown,
	}

	// Check for direct job view URLs: /jobs/view/123456
	jobViewRegex := regexp.MustCompile(`^/jobs/view/(\d+)/?$`)
	if matches := jobViewRegex.FindStringSubmatch(path); len(matches) > 1 {
		info.Type = LinkedInURLTypeJobView
		info.JobID = matches[1]
		info.PublicURL = fmt.Sprintf("https://www.linkedin.com/jobs/view/%s", info.JobID)
		return info, nil
	}

	// Check for job collection URLs: /jobs/collections/recommended/?currentJobId=123456
	if strings.HasPrefix(path, "/jobs/collections/") {
		if currentJobID := query.Get("currentJobId"); currentJobID != "" {
			// Validate that currentJobId is numeric
			jobIDRegex := regexp.MustCompile(`^\d+$`)
			if jobIDRegex.MatchString(currentJobID) {
				info.Type = LinkedInURLTypeJobCollection
				info.JobID = currentJobID
				info.PublicURL = fmt.Sprintf("https://www.linkedin.com/jobs/view/%s", info.JobID)
				return info, nil
			}
		}
		// Collection URL without valid job ID is non-job
		info.Type = LinkedInURLTypeNonJob
		return info, nil
	}

	// Check for other job-related paths that might be valid
	jobPaths := []string{
		"/jobs/view/",
		"/jobs/search/",
	}

	for _, jobPath := range jobPaths {
		if strings.HasPrefix(path, jobPath) {
			// If it starts with job path but doesn't match our patterns, it might still be job-related
			// But we'll be conservative and treat it as non-job unless it matches our specific patterns
			info.Type = LinkedInURLTypeNonJob
			return info, nil
		}
	}

	// All other LinkedIn URLs are non-job (profiles, company pages, feed, etc.)
	info.Type = LinkedInURLTypeNonJob
	return info, nil
}

// ConvertToPublicLinkedInJobURL converts various LinkedIn job URL formats to the public job view format
func ConvertToPublicLinkedInJobURL(urlStr string) (string, error) {
	info, err := ParseLinkedInURL(urlStr)
	if err != nil {
		return "", err
	}

	switch info.Type {
	case LinkedInURLTypeJobView:
		// Already a public job URL
		return info.PublicURL, nil
	case LinkedInURLTypeJobCollection:
		// Convert collection URL to public job URL
		return info.PublicURL, nil
	case LinkedInURLTypeNonJob:
		return "", NewNotJobPostingError(fmt.Sprintf("LinkedIn URL is not a job posting: %s", urlStr))
	default:
		return "", fmt.Errorf("unknown LinkedIn URL type for: %s", urlStr)
	}
}

// IsLinkedInJobURL checks if a LinkedIn URL is specifically a job posting URL
func IsLinkedInJobURL(urlStr string) bool {
	if !IsLinkedInURL(urlStr) {
		return false
	}

	info, err := ParseLinkedInURL(urlStr)
	if err != nil {
		return false
	}

	return info.Type == LinkedInURLTypeJobView || info.Type == LinkedInURLTypeJobCollection
}

// ExtractLinkedInJobID extracts the job ID from a LinkedIn job URL
func ExtractLinkedInJobID(urlStr string) (string, error) {
	info, err := ParseLinkedInURL(urlStr)
	if err != nil {
		return "", err
	}

	if info.JobID == "" {
		return "", fmt.Errorf("no job ID found in LinkedIn URL: %s", urlStr)
	}

	return info.JobID, nil
}
