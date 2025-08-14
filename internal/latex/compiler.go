package latex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Compile takes LaTeX source and compiles it to PDF using pdflatex.
// Returns the produced PDF bytes or an error containing the LaTeX log on failure.
func Compile(latexSource string) ([]byte, error) {
	if strings.TrimSpace(latexSource) == "" {
		return nil, fmt.Errorf("empty LaTeX source")
	}

	// If a remote PDF renderer is configured via env, use it
	if rendererURL := strings.TrimSpace(os.Getenv("PDF_RENDERER_URL")); rendererURL != "" {
		body, _ := json.Marshal(map[string]string{"latex": latexSource})
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(rendererURL, "/")+"/compile", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("renderer request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("renderer error: status=%d body=%s", resp.StatusCode, string(b))
		}
		pdf, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read renderer response: %w", err)
		}
		if len(pdf) == 0 {
			return nil, fmt.Errorf("renderer returned empty pdf")
		}
		return pdf, nil
	}

	// Create isolated working directory under tmp
	workDir, err := os.MkdirTemp("/app/tmp", "latex-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	// Clean up on return
	defer os.RemoveAll(workDir)

	// Write LaTeX to file
	texFile := filepath.Join(workDir, "document.tex")
	if err := os.WriteFile(texFile, []byte(latexSource), 0644); err != nil {
		return nil, fmt.Errorf("write tex file: %w", err)
	}

	// Prepare command; use nonstopmode and halt-on-error for deterministic behavior
	cmd := exec.Command("pdflatex", "-interaction=nonstopmode", "-halt-on-error", "-output-directory", workDir, texFile)
	cmd.Dir = workDir

	// Ensure TeX caches in writable location
	env := os.Environ()
	env = append(env, "TEXMFVAR=/app/tmp/texmf-var")
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		// Return combined output to help diagnose missing packages
		return nil, fmt.Errorf("pdflatex failed: %w; log:\n%s", err, out.String())
	}

	// Read produced PDF
	pdfPath := filepath.Join(workDir, "document.pdf")
	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w; log:\n%s", err, out.String())
	}

	return pdfBytes, nil
}
