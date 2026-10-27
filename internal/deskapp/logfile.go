package deskapp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// 常驻进程的可靠性兜底：
//   - 日志轮转（超 5MB 归档为 .1，避免无限增长）；
//   - 配置文件损坏时备份坏文件并用默认配置启动（托盘应用"起不来"比
//     "丢配置"更糟，且坏文件保留原样供用户找回配对记录）。

const (
	logMaxBytes   = 5 << 20
	logArchiveSuf = ".1"
)

// rotateWriter 是带大小轮转的追加日志写入器。
type rotateWriter struct {
	path    string
	max     int64
	f       *os.File
	written int64
}

// newRotateWriter 打开（或创建）日志文件。
func newRotateWriter(path string, maxSize int64) (*rotateWriter, error) {
	st, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	var written int64
	if err == nil && st != nil {
		written = st.Size()
	}
	return &rotateWriter{path: path, max: maxSize, f: f, written: written}, nil
}

func (w *rotateWriter) Write(p []byte) (int, error) {
	if w.written >= w.max {
		if err := w.rotate(); err != nil {
			// 轮转失败不阻塞日志：继续往原文件追加。
			n, err2 := w.f.Write(p)
			w.written += int64(n)
			return n, err2
		}
	}
	n, err := w.f.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *rotateWriter) rotate() error {
	_ = w.f.Close()
	_ = os.Rename(w.path, w.path+logArchiveSuf)
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.f = f
	w.written = 0
	return nil
}

func (w *rotateWriter) Close() error { return w.f.Close() }

// OpenRotatingLog 打开 GUI 入口的日志，超 5MB 自动归档轮转。
// configPath 非默认时日志名跟随配置文件名（<配置名>.log），多实例
// （多份配置同时跑 GUI，如自动化双开）日志不交织；默认路径仍是
// %LOCALAPPDATA%/winforge/desk.log。
func OpenRotatingLog(configPath string) (io.Writer, func(), error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "winforge"), 0o700); err != nil {
		return nil, nil, err
	}
	name := "desk.log"
	defaultCfg := filepath.Join(dir, "winforge", "desk.json")
	if configPath != "" && configPath != defaultCfg {
		base := filepath.Base(configPath)
		if ext := filepath.Ext(base); ext != "" {
			base = base[:len(base)-len(ext)]
		}
		if base != "" && base != "desk" {
			name = base + ".log"
		}
	}
	w, err := newRotateWriter(filepath.Join(dir, "winforge", name), logMaxBytes)
	if err != nil {
		return nil, nil, err
	}
	return w, func() { _ = w.Close() }, nil
}

// loadConfigWithRecovery 加载配置；解析失败时把坏文件归档为 .bad-<时间戳>，
// 用默认配置继续启动（返回 warning 供调用方 toast/打日志）。
// 文件不存在（首次运行）不属于损坏，正常返回默认配置、无警告。
func loadConfigWithRecovery(path string, defaultCfg func() error) (warn string, err error) {
	err = defaultCfg()
	if err == nil {
		return "", nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	// 解析/校验失败：备份坏文件后重建。
	stamped := fmt.Sprintf("%s.bad-%d", path, os.Getpid())
	if renameErr := os.Rename(path, stamped); renameErr == nil {
		warn = fmt.Sprintf("配置文件损坏，已备份为 %s 并使用默认配置启动", filepath.Base(stamped))
	}
	if err2 := defaultCfg(); err2 != nil {
		return warn, err2
	}
	return warn, nil
}
