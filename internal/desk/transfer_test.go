package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"报告.pdf", "报告.pdf"},
		{"a/b/c.txt", "c.txt"},
		{`..\..\evil.exe`, "evil.exe"},
		{"/etc/passwd", "passwd"},
		{"C:\\Windows\\system32\\config", "config"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"  spaced.txt  ", "spaced.txt"},
		{"trailing dots... ", "trailing dots"},
		{"con", "_con"},
		{"NUL.txt", "_NUL.txt"},
		{"a<b>:c?.txt", "a_b__c_.txt"},
		{"bad\x01name", "bad_name"},
	}
	for _, c := range cases {
		if got := sanitizeFileName(c.in); got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIncomingPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../evil", "..\\evil", "/abs/path", "a/../..//etc"} {
		if _, err := incomingPath(dir, name); err == nil {
			t.Errorf("应该拒绝 %q", name)
		}
	}
	p, err := incomingPath(dir, "ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !withinDir(dir, p) {
		t.Fatalf("结果 %s 不在目录内", p)
	}
}

func TestIncomingPathDedupes(t *testing.T) {
	dir := t.TempDir()
	p1, err := incomingPath(dir, "same.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := incomingPath(dir, "same.bin")
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatal("同名文件未去重")
	}
	if !strings.Contains(filepath.Base(p2), "(2)") {
		t.Fatalf("期望带序号: %s", p2)
	}
}

func TestIncomingFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var id [16]byte
	copy(id[:], "0123456789abcdef")
	data := bytes.Repeat([]byte("区块数据 "), 5000)
	f, err := newIncomingFile(dir, "数据.bin", int64(len(data)), id)
	if err != nil {
		t.Fatal(err)
	}
	// 分块写入。
	for i := 0; i < len(data); i += ChunkSize {
		end := i + ChunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := f.write(data[i:end]); err != nil {
			t.Fatal(err)
		}
	}
	path, sha, err := f.finish()
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if sha != hex.EncodeToString(want[:]) {
		t.Fatal("哈希不一致")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("内容不一致 err=%v len=%d", err, len(got))
	}
}

func TestIncomingFileAbortsRemoves(t *testing.T) {
	dir := t.TempDir()
	var id [16]byte
	f, err := newIncomingFile(dir, "aborted.bin", 100, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.write([]byte("partial"))
	f.abort()
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("半成品未被删除")
	}
}
