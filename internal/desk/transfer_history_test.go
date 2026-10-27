package desk

import (
	"fmt"
	"testing"
)

// 传输历史：emit 的 desk:transfer 事件进 Snapshot，同 id 合并，超上限淘汰最早。
func TestTransferHistoryInSnapshot(t *testing.T) {
	h := NewHub(testConfig(t), "", Platform{}, nil)

	h.emit("desk:transfer", map[string]interface{}{"id": "a", "state": "done", "name": "x.bin"})
	got, ok := h.Snapshot()["transfers"].([]map[string]interface{})
	if !ok || len(got) != 1 {
		t.Fatalf("snapshot 应含 1 条传输历史, got %v", got)
	}

	// 同 id 再发事件：合并不是新增
	h.emit("desk:transfer", map[string]interface{}{"id": "a", "recv": 5})
	got = h.Snapshot()["transfers"].([]map[string]interface{})
	if len(got) != 1 || got[0]["recv"] != 5 || got[0]["name"] != "x.bin" {
		t.Fatalf("同 id 应合并字段: %v", got[0])
	}

	// 无 id 的事件不入历史
	h.emit("desk:transfer", map[string]interface{}{"state": "done"})
	if n := len(h.Snapshot()["transfers"].([]map[string]interface{})); n != 1 {
		t.Fatalf("无 id 不应入历史, got %d", n)
	}

	// 超上限：最早的 a 被淘汰
	for i := 0; i < transferHistoryCap; i++ {
		h.emit("desk:transfer", map[string]interface{}{"id": fmt.Sprintf("t%d", i), "state": "done"})
	}
	got = h.Snapshot()["transfers"].([]map[string]interface{})
	if len(got) != transferHistoryCap {
		t.Fatalf("历史上限应为 %d, got %d", transferHistoryCap, len(got))
	}
	if got[0]["id"] != "t0" {
		t.Fatalf("最早一条应为 t0, got %v", got[0]["id"])
	}
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	cfg.UIPort = 0
	return cfg
}
