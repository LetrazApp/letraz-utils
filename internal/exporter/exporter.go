package exporter

import (
	"context"
	"errors"
	"fmt"

	"letraz-utils/internal/config"
	"letraz-utils/internal/latex"
	"letraz-utils/internal/logging"
	"letraz-utils/pkg/models"
	"letraz-utils/pkg/utils"
)

// Sentinel errors to allow precise mapping in handlers
var (
	ErrRender        = errors.New("render_error")
	ErrStorageConfig = errors.New("storage_configuration")
	ErrUpload        = errors.New("upload_failed")
)

// ExportResume renders a resume into LaTeX using the given theme and uploads the .tex file to Spaces.
// Returns the public URL of the uploaded file.
func ExportResume(_ context.Context, cfg *config.Config, resume models.BaseResume, theme string) (string, error) {
	logger := logging.GetGlobalLogger()

	// Render LaTeX
	engine := latex.NewEngine()
	latexStr, err := engine.Render(resume, theme)
	if err != nil {
		logger.Error("Failed to render LaTeX for export", map[string]interface{}{
			"resume_id": resume.ID,
			"theme":     theme,
			"error":     err.Error(),
		})
		return "", fmt.Errorf("%w: %v", ErrRender, err)
	}

	// Init Spaces client
	spaces, err := utils.NewSpacesClient(cfg)
	if err != nil {
		logger.Error("Storage not configured for export", map[string]interface{}{
			"resume_id": resume.ID,
			"error":     err.Error(),
		})
		return "", fmt.Errorf("%w: %v", ErrStorageConfig, err)
	}

	// Upload .tex
	url, err := spaces.UploadLatexExport(resume.ID, "", []byte(latexStr))
	if err != nil {
		logger.Error("Failed to upload LaTeX export", map[string]interface{}{
			"resume_id": resume.ID,
			"error":     err.Error(),
		})
		return "", fmt.Errorf("%w: %v", ErrUpload, err)
	}

	return url, nil
}
