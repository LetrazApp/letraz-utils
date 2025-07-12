package models

import "time"

// Suggestion represents a structured suggestion with metadata for resume improvement
type Suggestion struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // e.g., "experience", "skills", "profile", "education"
	Priority  string `json:"priority"`  // "high", "medium", "low"
	Impact    string `json:"impact"`    // Description of expected impact on job selection
	Section   string `json:"section"`   // Which section this applies to
	Current   string `json:"current"`   // Current state/content
	Suggested string `json:"suggested"` // Suggested improvement
	Reasoning string `json:"reasoning"` // Why this change would help
}

// User represents user information in a resume
type User struct {
	ID          string    `json:"id"`
	Title       *string   `json:"title"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone"`
	DOB         *string   `json:"dob"`
	Nationality *string   `json:"nationality"`
	Address     string    `json:"address"`
	City        string    `json:"city"`
	Postal      string    `json:"postal"`
	Country     *string   `json:"country"`
	Website     string    `json:"website"`
	ProfileText string    `json:"profile_text"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Country represents country information
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ExperienceData represents work experience information
type ExperienceData struct {
	ID               string    `json:"id"`
	User             string    `json:"user"`
	ResumeSection    string    `json:"resume_section"`
	CompanyName      string    `json:"company_name"`
	JobTitle         string    `json:"job_title"`
	EmploymentType   string    `json:"employment_type"`
	City             string    `json:"city"`
	Country          Country   `json:"country"`
	StartedFromMonth int       `json:"started_from_month"`
	StartedFromYear  int       `json:"started_from_year"`
	FinishedAtMonth  *int      `json:"finished_at_month"`
	FinishedAtYear   *int      `json:"finished_at_year"`
	Current          bool      `json:"current"`
	Description      string    `json:"description"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// EducationData represents education information
type EducationData struct {
	ID               string    `json:"id"`
	User             string    `json:"user"`
	ResumeSection    string    `json:"resume_section"`
	InstitutionName  string    `json:"institution_name"`
	FieldOfStudy     string    `json:"field_of_study"`
	Degree           string    `json:"degree"`
	Country          Country   `json:"country"`
	StartedFromMonth *int      `json:"started_from_month"`
	StartedFromYear  int       `json:"started_from_year"`
	FinishedAtMonth  *int      `json:"finished_at_month"`
	FinishedAtYear   int       `json:"finished_at_year"`
	Current          bool      `json:"current"`
	Description      string    `json:"description"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ResumeSection represents a section in a resume
type ResumeSection struct {
	ID     string      `json:"id"`
	Resume string      `json:"resume"`
	Index  int         `json:"index"`
	Type   string      `json:"type"`
	Data   interface{} `json:"data"` // Can be ExperienceData, EducationData, etc.
}

// BaseResume represents the complete resume structure
type BaseResume struct {
	ID       string          `json:"id"`
	Base     bool            `json:"base"`
	User     User            `json:"user"`
	Sections []ResumeSection `json:"sections"`
}

// TailorResumeRequest represents the request for resume tailoring
type TailorResumeRequest struct {
	BaseResume BaseResume `json:"base_resume"`
	Job        Job        `json:"job"`
	ResumeID   string     `json:"resume_id"`
}

// TailoredResume represents the tailored resume response
type TailoredResume struct {
	ID       string          `json:"id"`
	Base     bool            `json:"base"`
	User     User            `json:"user"`
	Sections []ResumeSection `json:"sections"`
}

// TailorResumeResponse represents the response for resume tailoring
type TailorResumeResponse struct {
	Success     bool           `json:"success"`
	Resume      TailoredResume `json:"resume"`
	Suggestions []Suggestion   `json:"suggestions"`
	ThreadID    string         `json:"threadId"`
	Error       string         `json:"error,omitempty"`
}
