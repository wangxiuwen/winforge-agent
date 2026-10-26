package desk

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Hub 编排 desk 的一切状态：发现、配对、会话、控制、传输、UI 事件。

// Emitter 把 UI 事件递给前端（Wails 事件桥或 HTTP WebSocket）。
type Emitter interface {
	Emit(event string, data interface{})
}

// Platform 抽象键鼠平台层，测试可注入假实现。
type Platform struct {
	// Injector 构造注入器（受控端）。
	Injector func() (Injector, error)
	// StartCapture 开始捕获键鼠（控制端），事件进 ch；返回停止函数。
	StartCapture func(ch chan InputEvent) (stop func(), err error)
	// WatchEdges 观察光标贴边意图，返回停止函数。
	WatchEdges func(cb func(edge Edge, yRatio float64)) (stop func(), err error)
	// Hotkey 全局热键触发信号（可为 nil）。
	Hotkey <-chan struct{}
	// Escape 本机物理 Esc 按下（被控端快捷交还用，可为 nil）。
	Escape <-chan struct{}
	// ScreenBounds 本机虚拟屏幕范围。
	ScreenBounds func() ScreenBounds
	// CursorPos 当前物理光标位置。
	CursorPos func() (int32, int32)
	// SetCursor 移动物理光标。
	SetCursor func(x, y int32)
	// SetControlIndicator 控制状态可视化（Windows: 屏幕边缘提示条）；可为 nil。
	SetControlIndicator func(on bool, controller bool, edge Edge)
}

// 控制角色。
const (
	roleIdle = iota
	roleController
	roleControlled
)

// ControlState 是键鼠控制的当前状态（对外可序列化）。
type ControlState struct {
	Role   int    `json:"role"` // 0 空闲 1 我在控制 2 被控制
	PeerID string `json:"peer,omitempty"`
	Peer   string `json:"peer_name,omitempty"`
}

// pendingPairIn 是收到的配对请求。
type pendingPairIn struct {
	ReqID string
	ID    string
	Name  string
	Code  string
	ch    chan bool
}

// pendingPairOut 是发起中的配对。
type pendingPairOut struct {
	Addr string
	Code string
	chn  chan bool
	open bool
}

// outgoingTransfer / incomingTransfer 供 UI 展示进度。
type outgoingTransfer struct {
	ID    [16]byte
	Peer  string
	Name  string
	Size  int64
	Sent  int64
	SHA   string
	State string // offering / sending / done / failed / rejected / cancelled
	Path  string
	file  *senderFile
	// cancel 由用户取消置位，发送循环逐块检查。
	cancel atomic.Bool
}

type incomingTransfer struct {
	ID    [16]byte
	Peer  string
	Name  string
	Size  int64
	Recv  int64
	SHA   string
	State string // offer / accepted / receiving / done / failed / rejected / cancelled
	Path  string
	file  *incomingFile
	// lastProgress 上次进度事件时间，用于限频。
	lastProgress time.Time
}

// Hub 持有 desk 全部运行状态。
type Hub struct {
	cfg     *Config
	cfgPath string
	mu      sync.Mutex
	log     *log.Logger

	platform Platform
	disc     *Discovery
	listener net.Listener

	sessions map[string]*Session

	pairReqs map[string]*pendingPairIn
	pairOut  *pendingPairOut

	ctl         ControlState
	savedX      int32
	savedY      int32
	integ       *Integrator
	captureCh   chan InputEvent
	captureStop func()
	ctlDone     chan struct{} // 本次控制会话的退出信号
	injector    Injector
	injKeys     *PressedKeys
	injButtons  map[MouseButton]bool

	out map[[16]byte]*outgoingTransfer
	in  map[[16]byte]*incomingTransfer

	tempUploads map[string]bool
	chatHistory map[string][]ChatMsg

	emittersMu sync.Mutex
	emitters   []Emitter

	edgeStop func()
	lastTry  map[string]time.Time
	wake     chan struct{}
	started  time.Time
	uiToken  string
	uiPort   int
}

// NewHub 构造。
func NewHub(cfg *Config, cfgPath string, platform Platform, logger *log.Logger) *Hub {
	if logger == nil {
		logger = log.Default()
	}
	return &Hub{
		cfg:         cfg,
		cfgPath:     cfgPath,
		platform:    platform,
		log:         logger,
		sessions:    map[string]*Session{},
		pairReqs:    map[string]*pendingPairIn{},
		injButtons:  map[MouseButton]bool{},
		out:         map[[16]byte]*outgoingTransfer{},
		in:          map[[16]byte]*incomingTransfer{},
		tempUploads: map[string]bool{},
		chatHistory: map[string][]ChatMsg{},
		lastTry:     map[string]time.Time{},
		wake:        make(chan struct{}, 1),
		started:     time.Now(),
		uiToken:     hex.EncodeToString(randomBytes(24)),
	}
}

// wakeSessions 立即触发一轮补连（配对/启用后立刻接上，不等周期）。
func (h *Hub) wakeSessions() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Token 返回本次运行的控制台访问令牌。
func (h *Hub) Token() string { return h.uiToken }

// SetUIPort 记录控制台实际端口。
func (h *Hub) SetUIPort(p int) { h.uiPort = p }

// UIPort 返回控制台端口。
func (h *Hub) UIPort() int { return h.uiPort }

// ControlPort 返回会话/配对 TCP 实际监听端口（Start 后有效）。
func (h *Hub) ControlPort() int {
	if h.listener == nil {
		return 0
	}
	return h.listener.Addr().(*net.TCPAddr).Port
}

// AddEmitter 注册 UI 事件接收方。
func (h *Hub) AddEmitter(e Emitter) {
	h.emittersMu.Lock()
	h.emitters = append(h.emitters, e)
	h.emittersMu.Unlock()
}

func (h *Hub) emit(event string, data interface{}) {
	h.emittersMu.Lock()
	list := append([]Emitter(nil), h.emitters...)
	h.emittersMu.Unlock()
	for _, e := range list {
		e.Emit(event, data)
	}
}

func (h *Hub) toast(level, text string) {
	h.log.Printf("[%s] %s", level, text)
	h.emit("desk:toast", map[string]string{"level": level, "text": text})
}

// Start 启动监听、发现与会话管理，直到 ctx 结束。
func (h *Hub) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", h.cfg.ControlPort))
	if err != nil {
		return fmt.Errorf("监听控制端口: %w", err)
	}
	h.listener = ln
	h.log.Printf("desk 控制端口 %d，ID %s，名称 %s", ln.Addr().(*net.TCPAddr).Port, h.cfg.ID, h.cfg.Name)

	discPort := h.cfg.DiscoveryPort
	controlPort := func() int { return ln.Addr().(*net.TCPAddr).Port }
	h.disc = NewDiscovery(h.cfg.ID, discPort, h.nameSnapshot, controlPort,
		h.pairedCountSnapshot, h.shareSnapshot, func(id string) bool {
			_, ok := h.cfg.Peer(id)
			return ok
		}, func([]Presence) { h.emitState() })
	if err := h.disc.Start(ctx, discPort); err != nil {
		h.log.Printf("UDP 发现不可用（不影响手动配对）: %v", err)
	}
	h.disc.StartPruner(ctx, 2*time.Second, 7*time.Second)

	h.loadChatHistory()
	safeGo(h.log, "runSessions", func() { h.runSessions(ctx) })
	safeGo(h.log, "acceptLoop", func() { h.acceptLoop(ctx) })
	safeGo(h.log, "watchHotkey", func() { h.watchHotkey(ctx) })
	safeGo(h.log, "watchEscape", func() { h.watchEscape(ctx) })
	h.startEdgeWatch()
	return nil
}

// startEdgeWatch 挂接边缘推挤观察（平台不支持时为空操作）。
func (h *Hub) startEdgeWatch() {
	if h.platform.WatchEdges == nil {
		return
	}
	stop, err := h.platform.WatchEdges(h.onEdge)
	if err != nil {
		h.log.Printf("边缘观察不可用: %v", err)
		return
	}
	h.mu.Lock()
	h.edgeStop = stop
	h.mu.Unlock()
}

// 三个 snapshot 由 discovery 的广播 goroutine 调用，
// 必须与 ToggleShare/savePairedPeer 等写方互斥（曾因此报 DATA RACE）。
func (h *Hub) nameSnapshot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.Name
}

func (h *Hub) pairedCountSnapshot() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.cfg.Peers)
}

func (h *Hub) shareSnapshot() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ShareEnabled
}

// acceptLoop 接受 TCP 连接：配对请求或会话握手。
func (h *Hub) acceptLoop(ctx context.Context) {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			h.log.Printf("accept: %v", err)
			return
		}
		go h.acceptOne(conn)
	}
}

func (h *Hub) acceptOne(conn net.Conn) {
	defer func() { _ = recover() }()
	_ = conn.SetDeadline(time.Now().Add(handshakeLimit))
	var first pairFrame
	if err := readJSONFrame(conn, &first); err != nil {
		_ = conn.Close()
		return
	}
	switch first.T {
	case msgPairRequest:
		go runPairingResponder(h, conn, first)
	case msgHello:
		h.acceptSession(conn, first)
	default:
		_ = conn.Close()
	}
}

// acceptSession 以被动方完成握手并进入服务循环。
func (h *Hub) acceptSession(conn net.Conn, hello pairFrame) {
	peer, ok := h.cfg.Peer(hello.ID)
	if !ok || peer.PSK == "" {
		_ = conn.Close()
		return
	}
	psk := unb64(peer.PSK)
	if len(psk) != 32 {
		_ = conn.Close()
		return
	}
	// 把已读出的 hello 交给 Session。
	s := &Session{hub: h, peer: peer, conn: conn, done: make(chan struct{})}
	// 复制一份握手：hello 已消费，这里改走专用流程。
	if err := s.handshakeFrom(hello, psk); err != nil {
		h.log.Printf("来自 %s 的会话握手失败: %v", hello.ID, err)
		_ = conn.Close()
		return
	}
	// 解除 accept 阶段的 10s 截止时间，长连接交给 serve 的读写策略。
	_ = conn.SetDeadline(time.Time{})
	h.registerSession(s)
	safeGo(h.log, "session-serve", s.serve)
}

// registerSession 登记会话（同一对端只保留最新一条）。
func (h *Hub) registerSession(s *Session) {
	h.mu.Lock()
	if old := h.sessions[s.peer.ID]; old != nil {
		old.close()
	}
	h.sessions[s.peer.ID] = s
	h.mu.Unlock()
	h.emitState()
}

// sessionClosed 会话结束：清理传输状态与控制状态。
func (h *Hub) sessionClosed(s *Session) {
	h.mu.Lock()
	if h.sessions[s.peer.ID] == s {
		delete(h.sessions, s.peer.ID)
	}
	if h.ctl.PeerID == s.peer.ID {
		role := h.ctl.Role
		h.cleanupControlLocked()
		h.mu.Unlock()
		h.controlChanged()
		h.toast("warn", "与 "+s.PeerName()+" 的连接断开，控制已复位")
		_ = role
		return
	}
	h.mu.Unlock()
	h.emitState()
}

// runSessions 周期补连所有启用的配对设备；wakeSessions 可即时触发。
func (h *Hub) runSessions(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.connectMissing()
		case <-h.wake:
			h.connectMissing()
		}
	}
}

func (h *Hub) connectMissing() {
	for _, p := range append([]Peer(nil), h.cfg.Peers...) {
		if !p.Enabled || p.PSK == "" {
			continue
		}
		h.mu.Lock()
		live := h.sessions[p.ID]
		last := h.lastTry[p.ID]
		h.mu.Unlock()
		if (live != nil && live.Alive()) || time.Since(last) < 3*time.Second {
			continue
		}
		h.mu.Lock()
		h.lastTry[p.ID] = time.Now()
		h.mu.Unlock()
		addr := p.Addr
		if addr == "" {
			for _, pr := range h.disc.List() {
				if pr.ID == p.ID && pr.Port > 0 && pr.Addr != "" {
					addr = net.JoinHostPort(pr.Addr, fmt.Sprint(pr.Port))
					break
				}
			}
		}
		if addr == "" {
			continue
		}
		h.dialSession(p, addr)
	}
}

// dialSession 主动连接一台配对设备。
func (h *Hub) dialSession(p Peer, addr string) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return
	}
	psk := unb64(p.PSK)
	if len(psk) != 32 {
		_ = conn.Close()
		return
	}
	s := &Session{hub: h, peer: p, conn: conn, done: make(chan struct{})}
	if err := s.handshake(psk, true); err != nil {
		h.log.Printf("连接 %s(%s) 握手失败: %v", p.Name, addr, err)
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	// 记住最近可用地址。
	h.mu.Lock()
	if peer, ok := h.cfg.Peer(p.ID); ok {
		peer.Addr = addr
		_ = h.cfg.UpPeer(h.cfgPath, peer)
	}
	h.mu.Unlock()
	h.registerSession(s)
	// serve 是永不返回的读循环，必须在独立 goroutine 里跑，
	// 否则 runSessions 的补连循环会被第一条会话永久卡死（多对端拨号失效）。
	safeGo(h.log, "session-serve", s.serve)
}

// sessionTo 并发安全的存活会话查询。
func (h *Hub) sessionTo(peerID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[peerID]
	if s != nil && !s.Alive() {
		return nil
	}
	return s
}

// controllingPeerID 返回当前控制状态涉及的对端（供 state 消息）。
func (h *Hub) controllingPeerID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role == roleIdle {
		return ""
	}
	return h.ctl.PeerID
}
