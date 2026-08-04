package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/config"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Config{
		Listen: "127.0.0.1:0", Root: t.TempDir(), Token: "test-token",
		MaxUploadBytes: 1 << 20, CommandTimeout: time.Minute,
	}
	s, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuthenticationIsRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	res := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", res.Code)
	}
}

func TestUploadIsAtomicAndSandboxed(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/upload?path=build/app.bin", bytes.NewReader([]byte("firmware")))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", res.Code, res.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(s.workspace.Root(), "build", "app.bin"))
	if err != nil || string(b) != "firmware" {
		t.Fatalf("file=%q err=%v", b, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/upload?path=../escape", bytes.NewReader([]byte("bad")))
	req.Header.Set("Authorization", "Bearer test-token")
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("traversal got %d", res.Code)
	}
}

func TestMkdirRejectsUnknownJSONFields(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"path": "ok", "unexpected": "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/mkdir", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	testServer(t).Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("got %d", res.Code)
	}
}
