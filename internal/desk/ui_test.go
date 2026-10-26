package desk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// testEmitter 收 Hub 事件供断言。
type testEmitter struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	name string
	data interface{}
}

func (e *testEmitter) Emit(n string, d interface{}) {
	e.mu.Lock()
	e.events = append(e.events, capturedEvent{n, d})
	e.mu.Unlock()
}

func (e *testEmitter) has(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.name == name {
			return true
		}
	}
	return false
}

func TestUIServerAuthAndStatic(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	h := NewHub(cfg, "unused", Platform{}, nil)
	addr, err := StartBrowserUI(h, 0)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	// 静态页不需要 token。
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte("WinForge Desk")) {
		t.Fatalf("首页异常: %d %s", resp.StatusCode, body[:min(len(body), 80)])
	}

	// 无 token 的 RPC 必须 401。
	resp, err = http.Post(base+"/api/rpc", "application/json", strings.NewReader(`{"cmd":"snapshot"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，实际 %d", resp.StatusCode)
	}

	// 错 token 也 401。
	resp, err = http.Get(base + "/api/events?t=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错 token 应 401，实际 %d", resp.StatusCode)
	}

	// 带 token 的 snapshot 正常。
	client := &http.Client{}
	req, _ := http.NewRequest("POST", base+"/api/rpc?t="+h.Token(), strings.NewReader(`{"cmd":"snapshot","args":[]}`))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var snap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap["self"] == nil {
		t.Fatal("快照缺 self")
	}

	// 事件长轮询能收到 emit 的事件。
	done := make(chan []uiEvent, 1)
	go func() {
		resp, err := http.Get(base + "/api/events?t=" + h.Token())
		if err != nil {
			done <- nil
			return
		}
		defer resp.Body.Close()
		var batch []uiEvent
		_ = json.NewDecoder(resp.Body).Decode(&batch)
		done <- batch
	}()
	time.Sleep(200 * time.Millisecond) // 等订阅建立
	h.emit("desk:toast", map[string]string{"level": "info", "text": "hello"})
	select {
	case batch := <-done:
		found := false
		for _, ev := range batch {
			if ev.Event == "desk:toast" {
				found = true
			}
		}
		if !found {
			t.Fatal("长轮询没收到事件")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("长轮询超时")
	}
}

func TestUIUploadAuth(t *testing.T) {
	cfg, _ := DefaultConfig()
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	h := NewHub(cfg, "unused", Platform{}, nil)
	addr, err := StartBrowserUI(h, 0)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	body := "-----X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\nContent-Type: text/plain\r\n\r\nhi\r\n-----X--\r\n"
	req, _ := http.NewRequest("POST", base+"/api/upload?peer=p1", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=---X")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 上传应 401，实际 %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", base+"/api/upload?peer=p1&t="+h.Token(), strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=---X")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// 对端不在线，应报业务错误但通过鉴权（非 401）。
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("带 token 不应 401")
	}
	fmt.Println("upload status:", resp.StatusCode)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
