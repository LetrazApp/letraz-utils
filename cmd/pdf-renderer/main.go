package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type compileRequest struct {
	Latex string `json:"latex"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func compileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req compileRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Latex) == "" {
		http.Error(w, "latex is required", http.StatusBadRequest)
		return
	}

	workDir, err := os.MkdirTemp("/tmp", "latex-build-*")
	if err != nil {
		http.Error(w, fmt.Sprintf("create temp dir: %v", err), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(workDir)

	texFile := filepath.Join(workDir, "document.tex")
	if err := os.WriteFile(texFile, []byte(req.Latex), 0644); err != nil {
		http.Error(w, fmt.Sprintf("write tex file: %v", err), http.StatusInternalServerError)
		return
	}

	// Prefer latexmk for multi-pass builds if available; fallback to pdflatex
	var out bytes.Buffer
	var cmd *exec.Cmd
	if _, err := exec.LookPath("latexmk"); err == nil {
		cmd = exec.Command("latexmk", "-pdf", "-interaction=nonstopmode", "-halt-on-error", "-outdir="+workDir, texFile)
	} else {
		cmd = exec.Command("pdflatex", "-interaction=nonstopmode", "-halt-on-error", "-output-directory", workDir, texFile)
	}
	cmd.Dir = workDir
	env := os.Environ()
	env = append(env, "TEXMFVAR=/tmp/texmf-var")
	cmd.Env = env
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("latex compile failed: %v\n%s", err, out.String()), http.StatusBadRequest)
		return
	}

	pdfPath := filepath.Join(workDir, "document.pdf")
	f, err := os.Open(pdfPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("read pdf: %v\n%s", err, out.String()), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/pdf")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("write response: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/compile", compileHandler)

	addr := ":8999"
	if v := os.Getenv("PORT"); strings.TrimSpace(v) != "" {
		addr = ":" + v
	}
	log.Printf("pdf-renderer listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
