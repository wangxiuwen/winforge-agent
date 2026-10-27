package desk

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 传输上限 max_file_bytes：发端超限本地拒绝，收端超限线上拒绝。
func TestMaxFileSizeRejected(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)

	data := make([]byte, 2048)
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	peerB := peerViews(a)[0]

	// 发端：上限 1KB，发送 2KB 应在本地被拒，B 侧无任何 offer。
	a.cfg.SetMaxFileBytes(1024)
	if err := a.SendFile(peerB.ID, src); err == nil {
		t.Fatal("发端超限应返回错误")
	}
	time.Sleep(300 * time.Millisecond)
	for _, v := range b.Snapshot()["transfers"].([]map[string]interface{}) {
		if v["state"] == "offer" {
			t.Fatalf("B 不应收到 offer: %v", v)
		}
	}

	// 收端：发端上限放大后可发出，但 B 上限 1KB 会线上拒绝 → A 侧 rejected。
	a.cfg.SetMaxFileBytes(1 << 20)
	b.cfg.SetMaxFileBytes(1024)
	if err := a.SendFile(peerB.ID, src); err != nil {
		t.Fatalf("发端上限内发送失败: %v", err)
	}
	waitFor(t, 5*time.Second, "A 收到对方拒绝", func() bool {
		for _, v := range a.Snapshot()["transfers"].([]map[string]interface{}) {
			if v["state"] == "rejected" {
				return true
			}
		}
		return false
	})
	entries, err := os.ReadDir(b.cfg.DownloadsDirV())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("B 下载目录应无文件, got %d", len(entries))
	}
}
