package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkspaceRejectsTraversalAndAbsolutePaths(t *testing.T) {
	w, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", filepath.Join("..", "secret"), filepath.Join(string(filepath.Separator), "tmp", "secret")} {
		if _, err := w.Resolve(path, true); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires an explicit privilege")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resolve(filepath.Join("escape", "file.bin"), true); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestWorkspaceAllowsMissingNestedTarget(t *testing.T) {
	w, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Resolve(filepath.Join("project", "out", "app.bin"), true)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "app.bin" {
		t.Fatalf("unexpected path: %s", got)
	}
}
