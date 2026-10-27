package desk

import (
	"strings"
	"testing"
	"time"
)

func TestHotkeyWarning(t *testing.T) {
	// 非平台宿主（nil 字段）：空串
	if got := HotkeyWarning(nil); got != "" {
		t.Fatalf("nil platform 应空串: %q", got)
	}
	if got := HotkeyWarning(&Platform{}); got != "" {
		t.Fatalf("无 HotkeyReady 应空串: %q", got)
	}
	// 注册失败：返回警告
	ready := make(chan struct{})
	close(ready)
	p := &Platform{HotkeyReady: ready, HotkeyFailed: func() bool { return true }}
	if got := HotkeyWarning(p); !strings.Contains(got, "热键注册失败") {
		t.Fatalf("失败应返回警告: %q", got)
	}
	// 注册成功：空串
	p2 := &Platform{HotkeyReady: ready, HotkeyFailed: func() bool { return false }}
	if got := HotkeyWarning(p2); got != "" {
		t.Fatalf("成功应空串: %q", got)
	}
	// 未就绪：2 秒超时返回超时警告
	p3 := &Platform{HotkeyReady: make(chan struct{})}
	start := time.Now()
	got := HotkeyWarning(p3)
	if got == "" || !strings.Contains(got, "超时") {
		t.Fatalf("未就绪应返回超时警告: %q", got)
	}
	if time.Since(start) < 1500*time.Millisecond {
		t.Fatalf("超时等待过短: %v", time.Since(start))
	}
}
