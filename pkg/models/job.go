package models

import "time"

// JobPosting represents a structured job posting extracted from job boards
type JobPosting struct {
	ID               string            `json:"id" validate:"required"`
	Title            string            `json:"title" validate:"required"`
	Company          string            `json:"company" validate:"required"`
	Location         string            `json:"location"`
	Remote           bool              `json:"remote"`
	Salary           *SalaryRange      `json:"salary,omitempty"`
	Description      string            `json:"description"`
	Requirements     []string          `json:"requirements"`
	Skills           []string          `json:"skills"`
	Benefits         []string          `json:"benefits"`
	ExperienceLevel  string            `json:"experience_level"`
	JobType          string            `json:"job_type"`
	PostedDate       time.Time         `json:"posted_date"`
	ApplicationURL   string            `json:"application_url"`
	Metadata         map[string]string `json:"metadata"`
	ProcessedAt      time.Time         `json:"processed_at"`
}

// SalaryRange represents the salary information for a job posting
type SalaryRange struct {
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Currency string `json:"currency"`
	Period   string `json:"period"` // hourly, monthly, yearly
} 