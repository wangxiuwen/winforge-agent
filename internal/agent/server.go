package agent

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/config"
)

type Server struct {
	cfg       config.Config
	workspace *Workspace
	logger    *log.Logger
}

type ExecRequest struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
}

type ExecEvent struct {
	Stream   string `json:"stream"`
	Data     string `json:"data,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

func New(cfg config.Config, logger *log.Logger) (*Server, error) {
	workspace, err := NewWorkspace(cfg.Root)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Server{cfg: cfg, workspace: workspace, logger: logger}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("POST /v1/exec", s.handleExec)
	mux.HandleFunc("POST /v1/upload", s.handleUpload)
	mux.HandleFunc("GET /v1/download", s.handleDownload)
	mux.HandleFunc("POST /v1/mkdir", s.handleMkdir)
	return s.logRequests(s.authenticate(mux))
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	s.logger.Printf("WinForge listening on https://%s root=%s", s.cfg.Listen, s.workspace.Root())
	err := httpServer.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(s.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s from=%s duration=%s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(started).Round(time.Millisecond))
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "winforge-agent", "os": runtime.GOOS, "arch": runtime.GOARCH,
		"hostname": hostname(), "root": s.workspace.Root(),
	})
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Command == "" || strings.ContainsAny(req.Command, "\r\n\x00") {
		http.Error(w, "command 不能为空或包含控制字符", http.StatusBadRequest)
		return
	}
	cwd, err := s.workspace.Resolve(req.Cwd, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	timeout := s.cfg.CommandTimeout
	if req.TimeoutMS > 0 {
		requested := time.Duration(req.TimeoutMS) * time.Millisecond
		if requested < timeout {
			timeout = requested
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), cleanEnv(req.Env)...)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, _ := w.(http.Flusher)
	events := make(chan ExecEvent, 64)
	var readers sync.WaitGroup
	readers.Add(2)
	go scanOutput(&readers, stdout, "stdout", events)
	go scanOutput(&readers, stderr, "stderr", events)
	go func() {
		readers.Wait()
		close(events)
	}()
	enc := json.NewEncoder(w)
	for event := range events {
		_ = enc.Encode(event)
		if flusher != nil {
			flusher.Flush()
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		terminateProcessTree(cmd)
		_ = enc.Encode(ExecEvent{Stream: "exit", ExitCode: -1, Error: ctx.Err().Error()})
		return
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	_ = enc.Encode(ExecEvent{Stream: "exit", ExitCode: exitCode})
}

func scanOutput(wg *sync.WaitGroup, reader io.Reader, stream string, events chan<- ExecEvent) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		events <- ExecEvent{Stream: stream, Data: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		events <- ExecEvent{Stream: stream, Error: err.Error()}
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	target, err := s.workspace.Resolve(r.URL.Query().Get("path"), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".winforge-upload-*")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	reader := http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	written, copyErr := io.Copy(tmp, reader)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		http.Error(w, "upload failed", http.StatusBadRequest)
		return
	}
	if err := os.Rename(tmpName, target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": r.URL.Query().Get("path"), "bytes": written})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	target, err := s.workspace.Resolve(r.URL.Query().Get("path"), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "只允许下载普通文件", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", mimeDisposition(filepath.Base(target)))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target, err := s.workspace.Resolve(req.Path, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(r io.Reader, out any) error {
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func cleanEnv(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		if key == "" || strings.ContainsAny(key, "=\x00\r\n") || strings.ContainsRune(value, '\x00') {
			continue
		}
		out = append(out, key+"="+value)
	}
	return out
}

func hostname() string {
	host, _ := os.Hostname()
	return host
}

func mimeDisposition(name string) string {
	return fmt.Sprintf("attachment; filename=%q", strings.ReplaceAll(name, `"`, ""))
}
