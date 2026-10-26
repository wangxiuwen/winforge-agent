package desk

import (
	"crypto/rand"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeInjector 记录注入调用，供控制流程断言（带锁，供 -race）。
type fakeInjector struct {
	mu       sync.Mutex
	moves    [][2]uint32
	cursors  [][2]int32
	released int
	bounds   ScreenBounds
}

func (f *fakeInjector) InjectMove(x, y uint32) {
	f.mu.Lock()
	f.moves = append(f.moves, [2]uint32{x, y})
	f.mu.Unlock()
}
func (f *fakeInjector) InjectButton(MouseButton, bool)       {}
func (f *fakeInjector) InjectWheel(int32)                    {}
func (f *fakeInjector) InjectKey(uint32, uint32, bool, bool) {}
func (f *fakeInjector) ScreenBounds() ScreenBounds           { return f.bounds }
func (f *fakeInjector) SetCursor(x, y int32) {
	f.mu.Lock()
	f.cursors = append(f.cursors, [2]int32{x, y})
	f.mu.Unlock()
}
func (f *fakeInjector) GetCursor() (int32, int32) { return 0, 0 }
func (f *fakeInjector) ReleaseAll()               { f.mu.Lock(); f.released++; f.mu.Unlock() }

func (f *fakeInjector) Moves() [][2]uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]uint32(nil), f.moves...)
}
func (f *fakeInjector) Cursors() [][2]int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]int32(nil), f.cursors...)
}
func (f *fakeInjector) Released() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

// fakePlatform 提供可控的捕获/注入。
type fakePlatform struct {
	mu       sync.Mutex
	inj      *fakeInjector
	capture  chan InputEvent
	hotkeyCh chan struct{}
	escapeCh chan struct{}
	cursor   [2]int32
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{
		inj:      &fakeInjector{bounds: ScreenBounds{X: 0, Y: 0, W: 1920, H: 1080}},
		capture:  make(chan InputEvent, 64),
		hotkeyCh: make(chan struct{}, 1),
		escapeCh: make(chan struct{}, 1),
	}
}

// sendCapture 往捕获通道投递事件（并发安全）。
func (f *fakePlatform) sendCapture(ev InputEvent) {
	f.mu.Lock()
	ch := f.capture
	f.mu.Unlock()
	if ch != nil {
		ch <- ev
	}
}

func (f *fakePlatform) toPlatform() Platform {
	return Platform{
		Injector: func() (Injector, error) { return f.inj, nil },
		StartCapture: func(ch chan InputEvent) (func(), error) {
			f.mu.Lock()
			f.capture = ch
			f.mu.Unlock()
			return func() { close(ch) }, nil
		},
		Hotkey:       f.hotkeyCh,
		Escape:       f.escapeCh,
		ScreenBounds: func() ScreenBounds { return ScreenBounds{X: 0, Y: 0, W: 1920, H: 1080} },
		CursorPos: func() (int32, int32) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.cursor[0], f.cursor[1]
		},
		SetCursor: func(x, y int32) {
			f.mu.Lock()
			f.cursor = [2]int32{x, y}
			f.mu.Unlock()
		},
	}
}

func newTestHub(t *testing.T, name string, p *fakePlatform) (*Hub, string) {
	t.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Name = name
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	cfg.UIPort = 0
	cfg.DownloadsDir = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "desk.json")
	if err := SaveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var plat Platform
	if p != nil {
		plat = p.toPlatform()
	} else {
		plat = Platform{}
	}
	h := NewHub(cfg, cfgPath, plat, log.New(os.Stderr, "["+name+"] ", log.LstdFlags))
	em := &testEmitter{}
	h.AddEmitter(em)
	registerTestEmitter(h, em)
	if err := h.Start(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	return h, cfgPath
}

// pairTwo 让 a 发起、b 接受，返回是否双方都落盘。
func pairTwo(t *testing.T, a, b *Hub) {
	t.Helper()
	addr := "127.0.0.1:" + itoa(b.ControlPort())
	if err := a.PairStart(addr); err != nil {
		t.Fatalf("发起配对: %v", err)
	}
	// B 出现配对请求。
	var reqID string
	waitFor(t, 5*time.Second, "B 收到配对请求", func() bool {
		snap := b.Snapshot()
		if pr, ok := snap["pairRequest"].(map[string]string); ok {
			reqID = pr["reqID"]
			return true
		}
		return false
	})
	// 双方配对码一致。
	codeB := ""
	if snap := b.Snapshot(); true {
		if pr, ok := snap["pairRequest"].(map[string]string); ok {
			codeB = pr["code"]
		}
	}
	codeA := ""
	if po, ok := a.Snapshot()["pairOut"].(map[string]string); ok {
		codeA = po["code"]
	}
	if codeA == "" || codeA != codeB {
		t.Fatalf("配对码不一致: A=%q B=%q", codeA, codeB)
	}
	b.PairRespond(reqID, true)
	a.PairConfirmOut(true)
	// 按 ID 等待（允许同一 Hub 已有其他配对）。
	idOf := func(h *Hub) string { return h.Snapshot()["self"].(map[string]interface{})["id"].(string) }
	aTarget, bTarget := idOf(b), idOf(a)
	onlineOf := func(h *Hub, id string) bool {
		for _, p := range peerViews(h) {
			if p.ID == id {
				return p.Online
			}
		}
		return false
	}
	waitFor(t, 5*time.Second, "A 保存对端", func() bool {
		for _, p := range peerViews(a) {
			if p.ID == aTarget {
				return true
			}
		}
		return false
	})
	waitFor(t, 5*time.Second, "B 保存对端", func() bool {
		for _, p := range peerViews(b) {
			if p.ID == bTarget {
				return true
			}
		}
		return false
	})
	// 等会话真正建立。
	waitFor(t, 8*time.Second, "A 侧会话在线", func() bool { return onlineOf(a, aTarget) })
	waitFor(t, 8*time.Second, "B 侧会话在线", func() bool { return onlineOf(b, bTarget) })
}

func peerViews(h *Hub) []PeerView {
	snap := h.Snapshot()
	pv, _ := snap["peers"].([]PeerView)
	return pv
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

func TestHubPairAndChat(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)

	peerB := peerViews(a)[0]
	if err := a.SendChat(peerB.ID, "你好，对端"); err != nil {
		t.Fatalf("发消息: %v", err)
	}
	waitFor(t, 5*time.Second, "B 收到消息", func() bool {
		return emitterOf(b).has("desk:chat")
	})
}

func TestHubFileTransfer(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)

	data := make([]byte, ChunkSize*3+123)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "源文件.bin")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	peerB := peerViews(a)[0]
	if err := a.SendFile(peerB.ID, src); err != nil {
		t.Fatalf("发送: %v", err)
	}
	// B 收到 offer。
	var fid string
	waitFor(t, 5*time.Second, "B 收到文件请求", func() bool {
		for _, ev := range emitterOf(b).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "in" && m["state"] == "offer" {
				fid, _ = m["id"].(string)
				return true
			}
		}
		return false
	})
	b.TransferRespond(fid, true)
	// A 侧发送完成。
	waitFor(t, 10*time.Second, "A 发送完成", func() bool {
		for _, ev := range emitterOf(a).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "out" && m["state"] == "sent" {
				return true
			}
		}
		return false
	})
	// B 侧落盘并校验。
	var recvPath string
	waitFor(t, 10*time.Second, "B 接收完成", func() bool {
		for _, ev := range emitterOf(b).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "in" && m["state"] == "done" {
				recvPath, _ = m["path"].(string)
				return true
			}
		}
		return false
	})
	got, err := os.ReadFile(recvPath)
	if err != nil {
		t.Fatalf("读取接收文件: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("长度不一致: %d vs %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("内容不一致 @%d", i)
		}
	}
}

func TestHubControlTakeRelease(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	// 设方位：B 在 A 右侧。
	peerB := peerViews(a)[0]
	if err := a.PeerPosition(peerB.ID, "right"); err != nil {
		t.Fatal(err)
	}
	peerA := peerViews(b)[0]
	if err := b.PeerPosition(peerA.ID, "left"); err != nil {
		t.Fatal(err)
	}

	if err := a.TakeControl(peerB.ID, 0.3); err != nil {
		t.Fatalf("发起控制: %v", err)
	}
	waitFor(t, 5*time.Second, "A 进入控制态", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleController
	})
	waitFor(t, 5*time.Second, "B 进入被控态", func() bool {
		return b.Snapshot()["control"].(ControlState).Role == roleControlled
	})
	// B 的光标应被放到自己的左边缘（A 在 B 左侧）。
	waitFor(t, 5*time.Second, "B 光标定位到共享边", func() bool {
		cs := pb.inj.Cursors()
		return len(cs) > 0 && cs[0][0] < 10
	})
	// A 捕获通道注入事件 → 转发 → B 注入。
	pa.sendCapture(InputEvent{Kind: InputMouseMove, MoveX: 30, MoveY: -10})
	pa.sendCapture(InputEvent{Kind: InputKey, VK: 0x41, Scan: 0x1E, Down: true})
	waitFor(t, 5*time.Second, "B 收到注入移动", func() bool { return len(pb.inj.Moves()) > 0 })

	// B 推边缘拿回控制。
	b.ReleaseControl(-1)
	waitFor(t, 5*time.Second, "双方回到空闲", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleIdle &&
			b.Snapshot()["control"].(ControlState).Role == roleIdle
	})
	if pb.inj.Released() == 0 {
		t.Fatal("被控结束应释放按键")
	}
}

func TestHubDenyControlWhenBusy(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	// B 共享开关关闭时应拒绝。
	b.ToggleShare(false)
	if err := a.TakeControl(peerB.ID, 0.5); err == nil {
		// 发起本身允许，但应很快收到 deny 回到空闲。
		waitFor(t, 5*time.Second, "A 被 deny 回到空闲", func() bool {
			return a.Snapshot()["control"].(ControlState).Role == roleIdle
		})
	}
}

func TestHubPeerRemoveDisconnects(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	if err := a.PeerRemove(peerB.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "A 会话断开", func() bool {
		return len(peerViews(a)) == 0
	})
	waitFor(t, 8*time.Second, "B 会话断开", func() bool {
		return len(peerViews(b)) == 0 || !peerViews(b)[0].Online
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestHubReconnectAfterDrop 拔线场景：会话被强制断开后自动重连、消息恢复。
func TestHubReconnectAfterDrop(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]

	// 模拟断线：直接关掉 A 侧会话的 TCP。
	a.mu.Lock()
	s := a.sessions[peerB.ID]
	a.mu.Unlock()
	if s == nil {
		t.Fatal("会话应在线")
	}
	_ = s.conn.Close()

	// 退避重连后恢复在线。
	waitFor(t, 15*time.Second, "断线后自动重连", func() bool {
		return len(peerViews(a)) == 1 && peerViews(a)[0].Online
	})
	// 重连后的会话能继续收发（切换窗口期可能出现一次瞬态失败，允许重试）。
	var chatErr error
	for i := 0; i < 20; i++ {
		chatErr = a.SendChat(peerB.ID, "重连后的消息")
		if chatErr == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if chatErr != nil {
		t.Fatalf("重连后发消息: %v", chatErr)
	}
	waitFor(t, 5*time.Second, "重连后消息送达", func() bool {
		for _, d := range emitterOf(b).all() {
			if m, ok := d.data.(map[string]interface{}); ok && d.name == "desk:chat" {
				if txt, _ := m["text"].(string); txt == "重连后的消息" {
					return true
				}
			}
		}
		return false
	})
}

// TestCancelTransfer 报价阶段取消：双方都到 cancelled，无残留。
// （回环上大文件瞬间传完，"传输中途"取消存在时序竞争，故取确定性的 offer 阶段。）
func TestCancelTransfer(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]

	data := make([]byte, ChunkSize*4)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "cancelme.bin")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.SendFile(peerB.ID, src); err != nil {
		t.Fatal(err)
	}
	// A 侧出现 offering 事件后立即取消（B 侧 offer 也已登记）。
	var fid string
	waitFor(t, 5*time.Second, "A 出现 offering", func() bool {
		for _, ev := range emitterOf(a).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "out" && m["state"] == "offering" {
				fid, _ = m["id"].(string)
				return true
			}
		}
		return false
	})
	waitFor(t, 5*time.Second, "B 收到 offer", func() bool {
		for _, ev := range emitterOf(b).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "in" && m["state"] == "offer" {
				return true
			}
		}
		return false
	})
	a.CancelTransfer(fid)
	waitFor(t, 5*time.Second, "A 状态变 cancelled", func() bool {
		for _, ev := range emitterOf(a).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "out" && m["state"] == "cancelled" {
				return true
			}
		}
		return false
	})
	waitFor(t, 5*time.Second, "B 状态变 cancelled", func() bool {
		for _, ev := range emitterOf(b).all() {
			if m, ok := ev.data.(map[string]interface{}); ok && m["dir"] == "in" && m["state"] == "cancelled" {
				return true
			}
		}
		return false
	})
	// 双方本地都无半成品文件。
	for _, h := range []*Hub{a, b} {
		dir := h.Snapshot()["downloads"].(string)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".bin" {
				t.Fatalf("半成品残留: %s", e.Name())
			}
		}
	}
}

// TestEscapeReleasesControl 被控端 Esc：立即交还。
func TestEscapeReleasesControl(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	if err := a.TakeControl(peerB.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "B 进入被控态", func() bool {
		return b.Snapshot()["control"].(ControlState).Role == roleControlled
	})
	// B 端物理 Esc。
	select {
	case pb.escapeCh <- struct{}{}:
	default:
	}
	waitFor(t, 5*time.Second, "双方回到空闲", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleIdle &&
			b.Snapshot()["control"].(ControlState).Role == roleIdle
	})
	if pb.inj.Released() == 0 {
		t.Fatal("交还时应释放按键")
	}
}

// TestMultiPeerTopology 三台拓扑：A 同时配对 B(右) 与 C(左)。
// 控制互斥（控制 B 时 C 拿不到 A）、交还后可换向控制。
func TestMultiPeerTopology(t *testing.T) {
	pa, pb, pc := newFakePlatform(), newFakePlatform(), newFakePlatform()
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	c, _ := newTestHub(t, "主机C", pc)
	pairTwo(t, a, b)
	pairTwo(t, a, c)

	idOf := func(h *Hub) string { return h.Snapshot()["self"].(map[string]interface{})["id"].(string) }
	bID, cID := idOf(b), idOf(c)
	peerOf := func(h *Hub, id string) string {
		for _, p := range peerViews(h) {
			if p.ID == id {
				return p.ID
			}
		}
		return ""
	}
	if peerOf(a, bID) == "" || peerOf(a, cID) == "" {
		t.Fatal("A 应同时配对 B 和 C")
	}
	// 方位：B 在 A 右侧、C 在 A 左侧；反过来 A 在 B 左、在 C 右。
	if err := a.PeerPosition(bID, "right"); err != nil {
		t.Fatal(err)
	}
	if err := a.PeerPosition(cID, "left"); err != nil {
		t.Fatal(err)
	}
	if err := b.PeerPosition(idOf(a), "left"); err != nil {
		t.Fatal(err)
	}
	if err := c.PeerPosition(idOf(a), "right"); err != nil {
		t.Fatal(err)
	}

	// A 控制 B：B 被控。
	if err := a.TakeControl(bID, 0.4); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "B 进入被控态", func() bool {
		return b.Snapshot()["control"].(ControlState).Role == roleControlled
	})
	// 控制互斥：此刻 C 想控制 A 应被拒（A 正忙于控制 B）。
	if err := c.TakeControl(idOf(a), 0.5); err == nil {
		waitFor(t, 5*time.Second, "C 的请求被拒、C 回空闲", func() bool {
			return c.Snapshot()["control"].(ControlState).Role == roleIdle
		})
	}
	waitFor(t, 5*time.Second, "A 仍处于控制态", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleController
	})
	// B 交还。
	b.ReleaseControl(-1)
	waitFor(t, 5*time.Second, "A/B 回到空闲", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleIdle &&
			b.Snapshot()["control"].(ControlState).Role == roleIdle
	})
	// 换向：A 控制 C。
	if err := a.TakeControl(cID, 0.4); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "C 进入被控态", func() bool {
		return c.Snapshot()["control"].(ControlState).Role == roleControlled
	})
	a.ReleaseControl(-1)
	waitFor(t, 5*time.Second, "全部回到空闲", func() bool {
		return a.Snapshot()["control"].(ControlState).Role == roleIdle &&
			b.Snapshot()["control"].(ControlState).Role == roleIdle &&
			c.Snapshot()["control"].(ControlState).Role == roleIdle
	})
}
