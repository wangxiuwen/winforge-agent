package desk

import (
	"testing"
	"time"
)

func TestDiscoveryUpsertAndPrune(t *testing.T) {
	updates := 0
	d := NewDiscovery("self", 41832,
		func() string { return "self-name" },
		func() int { return 40000 },
		func() int { return 0 },
		func() bool { return true },
		func(string) bool { return false },
		func([]Presence) { updates++ })
	if changed := d.upsert(announcePacket{V: 1, ID: "p1", Name: "  办公室主机\n", Port: 41001, Sharing: true}, "192.168.1.5"); !changed {
		t.Fatal("新增应报告变化")
	}
	if changed := d.upsert(announcePacket{V: 1, ID: "p1", Name: "办公室主机", Port: 41001, Sharing: true}, "192.168.1.6"); !changed {
		t.Fatal("更新应报告变化")
	}
	list := d.List()
	if len(list) != 1 {
		t.Fatalf("应有 1 台在线，实际 %d", len(list))
	}
	p := list[0]
	if p.Name != "办公室主机" || p.Addr != "192.168.1.6" || p.Port != 41001 || !p.Sharing {
		t.Fatalf("字段不对: %+v", p)
	}
	// 自己的广播被过滤由 Start 的读循环负责；这里只验 prune。
	d.mu.Lock()
	d.presence["p1"].LastSeen = time.Now().Add(-2 * time.Minute)
	d.mu.Unlock()
	if !d.Prune(7 * time.Second) {
		t.Fatal("超时设备应被清理")
	}
	if len(d.List()) != 0 {
		t.Fatal("清理后应为空")
	}
}

func TestSanitizeDisplayName(t *testing.T) {
	if got := sanitizeDisplayName("  a\nb  "); got != "a b" {
		t.Fatalf("换行未清洗: %q", got)
	}
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	if got := len([]rune(sanitizeDisplayName(string(long)))); got > 64 {
		t.Fatalf("名字过长: %d", got)
	}
}
