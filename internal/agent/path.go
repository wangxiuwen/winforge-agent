package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	root string
}

func NewWorkspace(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return &Workspace{root: filepath.Clean(real)}, nil
}

func (w *Workspace) Resolve(relative string, allowMissing bool) (string, error) {
	if relative == "" || relative == "." {
		return w.root, nil
	}
	if filepath.IsAbs(relative) {
		return "", errors.New("只接受 workspace 内的相对路径")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("路径越过 workspace")
	}
	candidate := filepath.Join(w.root, clean)
	check := candidate
	if allowMissing {
		for {
			if _, err := os.Lstat(check); err == nil {
				break
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", errors.New("无法解析目标路径")
			}
			check = parent
		}
	}
	real, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("符号链接越过 workspace")
	}
	return candidate, nil
}

func (w *Workspace) Root() string { return w.root }
