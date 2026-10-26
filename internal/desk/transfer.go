package desk

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 传输模型：发送方发 file_offer，接收方在 UI 确认后回 file_accept，
// 随后发送 FrameChunk 数据块，最后 file_done 带 SHA-256 校验。
// 接收方只写入配置的下载目录，文件名经 sanitizeFileName 清洗。

var (
	errTransferUnknown = errors.New("未知传输")
	errTransferSize    = errors.New("文件超过大小上限")
	errNameRejected    = errors.New("文件名非法")
)

// sanitizeFileName 清洗对端发来的文件名：去掉路径成分与危险字符，
// 拒绝空名、纯点、Windows 保留名。返回空串表示拒绝。
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return ""
	}
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if base == "" || base == "." || base == ".." {
		return ""
	}
	var b strings.Builder
	for _, r := range base {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	base = strings.TrimSpace(b.String())
	// 去掉结尾的空格/点（Windows 不允许）。
	base = strings.TrimRight(base, ". ")
	stem := base
	if i := strings.IndexByte(stem, '.'); i > 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		base = "_" + base
	}
	const maxName = 180
	if len([]rune(base)) > maxName {
		r := []rune(base)
		base = string(r[len(r)-maxName:])
	}
	if base == "" {
		return ""
	}
	return base
}

// incomingPath 生成接收文件的最终路径（不创建文件）。
// 路径形态的名字直接拒绝（防穿越），不做静默改写。
func incomingPath(dir, name string) (string, error) {
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", errNameRejected
	}
	clean := sanitizeFileName(name)
	if clean == "" {
		return "", errNameRejected
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	full := filepath.Join(dir, clean)
	// 双保险：结果必须仍在下载目录内。
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absFull, absDir+string(os.PathSeparator)) {
		return "", errNameRejected
	}
	// 同名文件自动加序号，避免覆盖。
	if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
		return full, nil
	}
	stem, ext := splitExt(clean)
	for i := 2; i < 10000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(cand); errors.Is(err, os.ErrNotExist) {
			return cand, nil
		}
	}
	return "", errors.New("无法分配不重名文件")
}

func splitExt(name string) (stem, ext string) {
	ext = filepath.Ext(name)
	return strings.TrimSuffix(name, ext), ext
}

// incomingFile 管理一个接收中的文件。
type incomingFile struct {
	id     [16]byte
	name   string
	path   string
	size   int64
	recv   int64
	file   *os.File
	digest hash.Hash
}

// newIncomingFile 打开接收文件。
func newIncomingFile(dir, name string, size int64, id [16]byte) (*incomingFile, error) {
	path, err := incomingPath(dir, name)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &incomingFile{
		id:     id,
		name:   name,
		path:   path,
		size:   size,
		file:   f,
		digest: sha256.New(),
	}, nil
}

// write 追加一个数据块。
func (t *incomingFile) write(data []byte) error {
	if t.recv+int64(len(data)) > t.size+int64(ChunkSize) { // 容忍末端对齐
		return errTransferSize
	}
	if _, err := t.file.Write(data); err != nil {
		return err
	}
	_, _ = t.digest.Write(data)
	t.recv += int64(len(data))
	return nil
}

// finish 校验并关闭，返回最终路径与哈希。
func (t *incomingFile) finish() (string, string, error) {
	if err := t.file.Sync(); err != nil {
		t.abort()
		return "", "", err
	}
	if err := t.file.Close(); err != nil {
		return "", "", err
	}
	t.file = nil
	return t.path, hex.EncodeToString(t.digest.Sum(nil)), nil
}

// abort 关闭并删除半成品。
func (t *incomingFile) abort() {
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
	}
	_ = os.Remove(t.path)
}

// senderFile 是发送方读取的本地文件。
type senderFile struct {
	f    *os.File
	size int64
	sha  string
}

// openSenderFile 打开并预计算哈希（上限内文件量级可接受）。
func openSenderFile(path string, maxSize int64) (*senderFile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.New("暂不支持发送目录")
	}
	if maxSize > 0 && st.Size() > maxSize {
		return nil, errTransferSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, st.Size())); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return &senderFile{f: f, size: st.Size(), sha: hex.EncodeToString(h.Sum(nil))}, nil
}

func (s *senderFile) Close() error { return s.f.Close() }
