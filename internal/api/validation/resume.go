package validation

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

// ResumeIDPattern validates resume IDs with format: rsm_ followed by alphanumeric chars, hyphens, and underscores
var ResumeIDPattern = regexp.MustCompile(`^rsm_[a-zA-Z0-9_-]{10,50}$`)

// ValidateResumeID validates that the resume ID follows the expected format
func ValidateResumeID(fl validator.FieldLevel) bool {
	resumeID := fl.Field().String()
	return ResumeIDPattern.MatchString(resumeID)
}

// RegisterResumeValidators registers all resume-related custom validators
func RegisterResumeValidators(v *validator.Validate) {
	v.RegisterValidation("resume_id", ValidateResumeID)
	// Placeholder: add theme validator when themes expand
}
