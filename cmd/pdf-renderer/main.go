package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
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

	// Bound request body size to prevent memory abuse
	const maxRequestBytes = 1 << 20 // 1 MiB
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

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

	// Validate input size and strip dangerous primitives
	if len(req.Latex) > 500_000 { // ~500 KB cap for LaTeX source
		http.Error(w, "latex input too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := validateLatex(req.Latex); err != nil {
		http.Error(w, fmt.Sprintf("latex rejected: %v", err), http.StatusBadRequest)
		return
	}

	workDir, err := os.MkdirTemp("/tmp", "latex-build-*")
	if err != nil {
		http.Error(w, fmt.Sprintf("create temp dir: %v", err), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(workDir)

	texFile := filepath.Join(workDir, "document.tex")
	if err := os.WriteFile(texFile, []byte(req.Latex), 0600); err != nil {
		http.Error(w, fmt.Sprintf("write tex file: %v", err), http.StatusInternalServerError)
		return
	}

	// Build command and enforce security mitigations
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd, err := buildLatexCommand(ctx, workDir, texFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("build command: %v", err), http.StatusInternalServerError)
		return
	}
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		// Kill entire process group on timeout or error
		if ctx.Err() == context.DeadlineExceeded && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
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

// buildLatexCommand constructs the LaTeX compilation command with security mitigations.
func buildLatexCommand(ctx context.Context, workDir, texFile string) (*exec.Cmd, error) {
	// Compose the base LaTeX command with shell-escape disabled
	var args []string
	if _, err := exec.LookPath("latexmk"); err == nil {
		// Ensure pdflatex invoked by latexmk also has -no-shell-escape
		pdflatex := "pdflatex -interaction=nonstopmode -halt-on-error -no-shell-escape"
		args = []string{
			"latexmk",
			"-pdf",
			"-interaction=nonstopmode",
			"-halt-on-error",
			"-outdir=" + workDir,
			"-pdflatex=" + pdflatex,
			texFile,
		}
	} else {
		args = []string{
			"pdflatex",
			"-interaction=nonstopmode",
			"-halt-on-error",
			"-no-shell-escape",
			"-output-directory",
			workDir,
			texFile,
		}
	}

	// Apply OS-level resource limits via a shell wrapper (portable)
	// Limits: CPU seconds, virtual memory, max output file size, file descriptors
	maxCPUSeconds := 20
	maxAddressSpaceKB := 512 * 1024 // 512 MiB
	maxPDFBytes := int64(200 * 1024 * 1024)
	// ulimit -f expects 512-byte blocks on most systems
	maxFileBlocks := (maxPDFBytes + 511) / 512

	latexCmdStr := shellJoin(args)
	shCmd := fmt.Sprintf("ulimit -t %d; ulimit -v %d; ulimit -f %d; ulimit -n 32; exec %s", maxCPUSeconds, maxAddressSpaceKB, maxFileBlocks, latexCmdStr)

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", shCmd)
	cmd.Dir = workDir

	// Minimal, controlled environment
	env := []string{
		"PATH=/usr/bin:/bin:/usr/local/bin",
		"HOME=" + workDir,
		"TEXMFVAR=" + filepath.Join(workDir, "texmf-var"),
		// Avoid inheriting proxies or other sensitive env vars
		"NO_PROXY=*",
		"http_proxy=",
		"https_proxy=",
	}
	// Preserve locale if present to avoid LaTeX font issues
	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		if val, ok := lookupEnvExact(key); ok {
			env = append(env, key+"="+val)
		}
	}
	cmd.Env = env

	// Create a new process group so we can kill children
	sys := &syscall.SysProcAttr{Setpgid: true}

	// Drop privileges if running as root
	if os.Geteuid() == 0 {
		if cred, err := nobodyCredential(); err == nil {
			sys.Credential = cred
		}
		// On Linux we could also set seccomp/apparmor here via containerization; omitted here.
	}
	cmd.SysProcAttr = sys

	return cmd, nil
}

func lookupEnvExact(key string) (string, bool) {
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && kv[:i] == key {
			return kv[i+1:], true
		}
	}
	return "", false
}

func nobodyCredential() (*syscall.Credential, error) {
	u, err := user.Lookup("nobody")
	if err != nil {
		return nil, err
	}
	uid64, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	gid64, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, err
	}
	cred := &syscall.Credential{Uid: uint32(uid64), Gid: uint32(gid64)}
	return cred, nil
}

// validateLatex performs simple static checks to reject dangerous primitives and paths.
func validateLatex(src string) error {
	s := src
	if len(s) == 0 {
		return errors.New("empty input")
	}
	// Normalize to lower for simple substring checks where case-insensitive
	lower := strings.ToLower(s)

	// Denylist of obviously dangerous primitives
	denySubs := []string{
		`\\write18`,
		`\\openout`,
		`\\openin`,
		`\\read`,
		`\\immediate\\s*\\write`,
	}
	for _, pat := range denySubs {
		re := regexp.MustCompile(pat)
		if re.MatchString(lower) {
			return fmt.Errorf("contains forbidden primitive: %s", pat)
		}
	}

	// Disallow packages known to re-enable shell-escape or file IO conveniences
	badPkgs := []string{"shellesc", "write18", "catchfile", "verbatiminput"}
	for _, p := range badPkgs {
		re := regexp.MustCompile(`\\usepackage\s*\{[^}]*` + regexp.QuoteMeta(p) + `[^}]*\}`)
		if re.MatchString(lower) {
			return fmt.Errorf("forbidden package: %s", p)
		}
	}

	// Block \input or \include of absolute paths or URLs
	reInput := regexp.MustCompile(`\\(input|include)\s*\{([^}]*)\}`)
	matches := reInput.FindAllStringSubmatch(lower, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			arg := strings.TrimSpace(m[2])
			if strings.HasPrefix(arg, "/") || strings.Contains(arg, `://`) || strings.Contains(arg, `..`) {
				return fmt.Errorf("forbidden include path: %s", arg)
			}
		}
	}

	// Basic cap on number of includes to avoid pathological recursion
	if len(matches) > 32 {
		return fmt.Errorf("too many includes: %d", len(matches))
	}

	// On non-Unix platforms or unusual runtimes, be extra conservative
	if runtime.GOOS == "windows" {
		return errors.New("unsupported platform")
	}
	return nil
}

// shellJoin safely quotes arguments for a POSIX shell.
func shellJoin(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, shellQuote(a))
	}
	return strings.Join(out, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// Replace every ' with '\'' and wrap in single quotes
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
