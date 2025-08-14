package latex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"letraz-utils/internal/config"
)

// Compile takes LaTeX source and compiles it to PDF using pdflatex.
// Returns the produced PDF bytes or an error containing the LaTeX log on failure.
func Compile(cfg *config.Config, latexSource string) ([]byte, error) {
	if strings.TrimSpace(latexSource) == "" {
		return nil, fmt.Errorf("empty LaTeX source")
	}

	// If a remote PDF renderer is configured via config, use it
	if rendererURL := strings.TrimSpace(cfg.PDFRenderer.URL); rendererURL != "" {
		body, err := json.Marshal(map[string]string{"latex": latexSource})
		if err != nil {
			return nil, fmt.Errorf("marshal latex payload: %w", err)
		}
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(rendererURL, "/")+"/compile", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		// Use timeout from configuration, with a sane default if unset
		clientTimeout := 30 * time.Second
		if cfg != nil && cfg.PDFRenderer.Timeout > 0 {
			clientTimeout = cfg.PDFRenderer.Timeout
		}
		client := &http.Client{Timeout: clientTimeout}
		resp, err := client.Do(req)
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
	// Use a context with timeout so runaway compilations are killed
	compileTimeout := 30 * time.Second
	if cfg != nil && cfg.PDFRenderer.Timeout > 0 {
		compileTimeout = cfg.PDFRenderer.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), compileTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdflatex", "-interaction=nonstopmode", "-halt-on-error", "-no-shell-escape", "-output-directory", workDir, texFile)
	cmd.Dir = workDir

	// Ensure TeX caches in writable location
	env := os.Environ()
	env = append(env, "TEXMFVAR=/app/tmp/texmf-var")
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		// Return combined output to help diagnose issues and handle timeouts clearly
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("pdflatex timed out after %s: %v; log:\n%s", compileTimeout, err, out.String())
		}
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
