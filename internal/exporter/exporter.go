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
	ErrCompile       = errors.New("compile_error")
	ErrStorageConfig = errors.New("storage_configuration")
	ErrUpload        = errors.New("upload_failed")
)

// ExportResume renders a resume into LaTeX using the given theme, compiles a PDF,
// uploads both artifacts to Spaces, and returns their public URLs (latexURL, pdfURL).
func ExportResume(_ context.Context, cfg *config.Config, resume models.BaseResume, theme string) (string, string, error) {
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
		return "", "", fmt.Errorf("%w: %v", ErrRender, err)
	}

	// Compile PDF using LaTeX toolchain
	pdfBytes, err := latex.Compile(cfg, latexStr)
	if err != nil {
		logger.Error("Failed to compile LaTeX to PDF", map[string]interface{}{
			"resume_id": resume.ID,
			"theme":     theme,
			"error":     err.Error(),
		})
		return "", "", fmt.Errorf("%w: %v", ErrCompile, err)
	}

	// Init Spaces client
	spaces, err := utils.NewSpacesClient(cfg)
	if err != nil {
		logger.Error("Storage not configured for export", map[string]interface{}{
			"resume_id": resume.ID,
			"error":     err.Error(),
		})
		return "", "", fmt.Errorf("%w: %v", ErrStorageConfig, err)
	}

	// To keep both files paired, generate a common base filename
	baseFile := utils.GenerateRequestID()
	texName := baseFile + ".tex"
	pdfName := baseFile + ".pdf"

	// Upload .tex
	latexURL, err := spaces.UploadLatexExport(resume.ID, texName, []byte(latexStr))
	if err != nil {
		logger.Error("Failed to upload LaTeX export", map[string]interface{}{
			"resume_id": resume.ID,
			"tex_name":  texName,
			"error":     err.Error(),
		})
		return "", "", ErrUpload
	}
	// Upload .pdf
	pdfURL, err := spaces.UploadPDFExport(resume.ID, pdfName, pdfBytes)
	if err != nil {
		// Best-effort cleanup of previously uploaded .tex
		_ = spaces.DeleteExportObject(resume.ID, texName)
		logger.Error("Failed to upload PDF export", map[string]interface{}{
			"resume_id": resume.ID,
			"pdf_name":  pdfName,
			"error":     err.Error(),
		})
		return "", "", ErrUpload
	}

	return latexURL, pdfURL, nil
}
