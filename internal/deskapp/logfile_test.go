package deskapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.log")
	w, err := newRotateWriter(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := strings.Repeat("x", 60) + "\n"
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if w.written > 200 {
		t.Fatal("轮转后 written 未清零")
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("归档文件缺失: %v", err)
	}
	b, err := os.ReadFile(path + ".1")
	if err != nil || len(b) == 0 {
		t.Fatalf("归档为空: %v", err)
	}
}

func TestLoadConfigWithRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desk.json")
	if err := os.WriteFile(path, []byte("{broken json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 模拟 LoadConfig 的行为：读文件解析失败返回错误；重建后成功。
	calls := 0
	defaultCfg := func() error {
		calls++
		if calls == 1 {
			return errors.New("解析失败") // 第一次：坏文件
		}
		return nil // 第二次：重建后的默认配置
	}
	warn, err := loadConfigWithRecovery(path, defaultCfg)
	if err != nil {
		t.Fatalf("自愈后仍失败: %v", err)
	}
	if warn == "" {
		t.Fatal("应有损坏警告")
	}
	if calls != 2 {
		t.Fatalf("defaultCfg 应被调用两次，实际 %d", calls)
	}
	// 坏文件应被归档（不留在原路径）。
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("原路径应已被重建流程接管")
	}
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bad-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("坏文件未被备份: %v", entries)
	}
}
