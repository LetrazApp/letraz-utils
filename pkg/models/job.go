package models

import "time"

// Job represents a structured job posting extracted from job boards
// This matches the requested structure from the user
type Job struct {
	Title            string   `json:"title"`
	JobURL           string   `json:"job_url"`
	CompanyName      string   `json:"company_name"`
	Location         string   `json:"location"`
	Currency         string   `json:"currency"`
	SalaryMax        *int     `json:"salary_max"`
	SalaryMin        *int     `json:"salary_min"`
	Salary           Salary   `json:"salary"`
	Requirements     []string `json:"requirements"`
	Description      string   `json:"description"`
	Responsibilities []string `json:"responsibilities"`
	Benefits         []string `json:"benefits"`
}

// Salary represents the salary information for a job posting
type Salary struct {
	Currency string `json:"currency"`
	Max      int    `json:"max"`
	Min      int    `json:"min"`
}

// JobPosting represents a structured job posting extracted from job boards (legacy)
// Keep this for backward compatibility during transition
type JobPosting struct {
	ID              string            `json:"id" validate:"required"`
	Title           string            `json:"title" validate:"required"`
	Company         string            `json:"company" validate:"required"`
	Location        string            `json:"location"`
	Remote          bool              `json:"remote"`
	Salary          *SalaryRange      `json:"salary,omitempty"`
	Description     string            `json:"description"`
	Requirements    []string          `json:"requirements"`
	Skills          []string          `json:"skills"`
	Benefits        []string          `json:"benefits"`
	ExperienceLevel string            `json:"experience_level"`
	JobType         string            `json:"job_type"`
	PostedDate      time.Time         `json:"posted_date"`
	ApplicationURL  string            `json:"application_url"`
	Metadata        map[string]string `json:"metadata"`
	ProcessedAt     time.Time         `json:"processed_at"`
}

// SalaryRange represents the salary information for a job posting (legacy)
type SalaryRange struct {
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Currency string `json:"currency"`
	Period   string `json:"period"` // hourly, monthly, yearly
}
