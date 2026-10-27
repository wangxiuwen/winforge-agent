package desk

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// HTTP 层断点续传：/api/events?after=N 只返回 seq>N 的事件（含回放），
// 不带 after 只返回实时。bus 层逻辑在 TestUIBusReplayAfter 已覆盖，
// 这里钉住 handleEvents 的参数解析与 flush 路径。
func TestUIEventsHTTPAfter(t *testing.T) {
	cfg, _ := DefaultConfig()
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	h := NewHub(cfg, "unused", Platform{}, nil)
	addr, err := StartBrowserUI(h, 0)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	h.emit("desk:chat", map[string]string{"n": "e1"})
	h.emit("desk:chat", map[string]string{"n": "e2"})
	h.emit("desk:chat", map[string]string{"n": "e3"})

	poll := func(url string) []struct {
		Seq   int    `json:"seq"`
		Event string `json:"event"`
	} {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var batch []struct {
			Seq   int    `json:"seq"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(b, &batch); err != nil {
			t.Fatalf("bad batch %q: %v", b, err)
		}
		return batch
	}

	// after=2 → 只回放 3；通道无历史订阅冲突，回放后服务端 hold 到 tick 才 flush。
	batch := poll(base + "/api/events?t=" + h.Token() + "&after=2")
	if len(batch) != 1 || batch[0].Seq != 3 {
		t.Fatalf("after=2 应只回放 seq=3, got %+v", batch)
	}

	// 不带 after → 只投实时（历史不回放）；服务端 25s hold 太久，用手动
	// 超时客户端验证"立即不回放"。
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/api/events?t=" + h.Token())
	if err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var batch []struct {
			Seq int `json:"seq"`
		}
		_ = json.Unmarshal(b, &batch)
		if len(batch) != 0 {
			t.Fatalf("缺省 after 不应回放, got %+v", batch)
		}
	}
	// err != nil = 2s 超时断开（hold 中），同样证明没有立即回放。
}
