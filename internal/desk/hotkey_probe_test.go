//go:build windows

package desk

import (
	"log"
	"testing"
	"time"
)

func TestHotkeyWarningRealPlatform(t *testing.T) {
	p := NewPlatform("Ctrl+Alt+D", log.Default())
	got := HotkeyWarning(p)
	t.Logf("HotkeyWarning = %q", got)
	if got == "" {
		t.Fatal("真实平台上注册失败（被测试进程组占用/主实例占用）应返回警告")
	}
	_ = time.Now()
}
