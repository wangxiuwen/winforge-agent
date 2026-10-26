package desk

import (
	"context"
	"errors"
	"time"
)

// 错误定义。
var (
	errBusy         = errors.New("当前已处于共享状态")
	errNotConnected = errors.New("对端不在线")
	errPeerDisabled = errors.New("对端已被停用")
)

// 控制模型：同一时刻一对设备间至多一个方向在控制。
//   roleController：本机键鼠被捕获并转发给对端注入；
//   roleControlled：本机接收注入；本地真实输入照常。
// 切换手段：屏幕边缘推挤（双方都装了边缘观察）、全局热键、UI 按钮。

// controlChanged 控制状态变化：发 UI 事件 + 驱动边缘提示条。
func (h *Hub) controlChanged() {
	h.emit("desk:control", h.ctl)
	if h.platform.SetControlIndicator == nil {
		return
	}
	switch h.ctl.Role {
	case roleController:
		h.platform.SetControlIndicator(true, true, h.controlEdge())
	case roleControlled:
		h.platform.SetControlIndicator(true, false, h.controlEdge())
	default:
		h.platform.SetControlIndicator(false, false, EdgeNone)
	}
}

// controlEdge 当前控制状态对应的共享边；无方位时右侧兜底（保证有可见反馈）。
func (h *Hub) controlEdge() Edge {
	if p, ok := h.cfg.Peer(h.ctl.PeerID); ok && p.Position != "" {
		return edgeForPosition(p.Position)
	}
	return EdgeRight
}

// onEdge 边缘意图回调（平台层已做去抖）。
func (h *Hub) onEdge(edge Edge, yRatio float64) {
	h.mu.Lock()
	role := h.ctl.Role
	h.mu.Unlock()
	switch role {
	case roleIdle:
		// 找该方位上启用的对端 → 发起控制。
		for _, p := range h.cfg.Peers {
			if !p.Enabled || p.Position == "" {
				continue
			}
			if edgeForPosition(p.Position) == edge {
				h.TakeControl(p.ID, yRatio)
				return
			}
		}
	case roleControlled:
		// 我正被控制：朝控制器侧的边推 = 拿回；朝另一侧有配对设备的边推 = 链式切换。
		h.mu.Lock()
		peerID := h.ctl.PeerID
		h.mu.Unlock()
		peer, ok := h.cfg.Peer(peerID)
		if !ok {
			return
		}
		if peer.Position != "" && edgeForPosition(peer.Position) == edge {
			h.ReleaseControl(yRatio)
			return
		}
		// 链式：本机另一侧的方位对端，请控制器把输入链切过去。
		for _, p := range h.cfg.Peers {
			if p.Enabled && p.ID != peerID && p.Position != "" && edgeForPosition(p.Position) == edge {
				if s := h.sessionTo(peerID); s != nil {
					_ = s.sendJSON(wireMsg{T: msgCtlChain, FID: p.ID, Y: yRatio})
				}
				return
			}
		}
	}
}

// edgeForPosition 对端方位对应本机哪条边。
func edgeForPosition(pos string) Edge {
	if pos == "left" {
		return EdgeLeft
	}
	if pos == "right" {
		return EdgeRight
	}
	return EdgeNone
}

// TakeControl 请求控制对端。yRatio 是进入对端时的光标纵向比例（0=顶）。
func (h *Hub) TakeControl(peerID string, yRatio float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role != roleIdle {
		return errBusy
	}
	s := h.sessions[peerID]
	if s == nil || !s.Alive() {
		return errNotConnected
	}
	peer, ok := h.cfg.Peer(peerID)
	if !ok || !peer.Enabled {
		return errPeerDisabled
	}
	h.ctl = ControlState{Role: roleController, PeerID: peerID, Peer: s.PeerName()}
	x, y := h.platform.CursorPos()
	h.savedX, h.savedY = x, y
	h.integ = NewIntegrator(h.platform.ScreenBounds(), float64(x), float64(y))
	if yRatio < 0 {
		yRatio = cursorYRatio(h.platform.ScreenBounds(), y)
	}
	h.controlChanged()
	go func() { _ = s.sendJSON(wireMsg{T: msgCtlTake, Y: yRatio}) }()
	return nil
}

// onCtlTake 对端想控制我。
func (h *Hub) onCtlTake(s *Session, m wireMsg) {
	h.mu.Lock()
	if h.ctl.Role != roleIdle || !h.cfg.ShareEnabled {
		h.mu.Unlock()
		_ = s.sendJSON(wireMsg{T: msgCtlDeny})
		return
	}
	inj, err := h.platform.Injector()
	if err != nil {
		h.mu.Unlock()
		_ = s.sendJSON(wireMsg{T: msgCtlDeny})
		h.toast("error", "注入器初始化失败: "+err.Error())
		return
	}
	h.ctl = ControlState{Role: roleControlled, PeerID: s.peer.ID, Peer: s.PeerName()}
	h.injector = inj
	h.injKeys = NewPressedKeys()
	h.injButtons = map[MouseButton]bool{}
	h.placeCursorForEntryLocked(s.peer.ID, m.Y)
	h.controlChanged()
	h.mu.Unlock()
	_ = s.sendJSON(wireMsg{T: msgCtlTakeOK})
	h.toast("info", s.PeerName()+" 正在控制这台机器（推开共享边或按热键拿回）")
}

// placeCursorForEntryLocked 控制进入时把光标放到共享边。
func (h *Hub) placeCursorForEntryLocked(peerID string, yRatio float64) {
	if h.injector == nil {
		return
	}
	b := h.injector.ScreenBounds()
	y := b.Y + int32(EdgeEntryYRatio(yRatio)*float64(b.H))
	var x int32
	switch h.peerPosition(peerID) {
	case "left":
		x = b.X + 2
	case "right":
		x = b.X + b.W - 2
	default:
		x = b.X + b.W/2
	}
	h.injector.SetCursor(x, y)
}

// peerPosition 读取对端方位（锁外安全快照）。
func (h *Hub) peerPosition(peerID string) string {
	if p, ok := h.cfg.Peer(peerID); ok {
		return p.Position
	}
	return ""
}

// onCtlTakeOK 我方发起的控制被接受：开始捕获。
func (h *Hub) onCtlTakeOK(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role != roleController || h.ctl.PeerID != s.peer.ID {
		return
	}
	ch := make(chan InputEvent, 512)
	stop, err := h.platform.StartCapture(ch)
	if err != nil {
		h.ctl = ControlState{Role: roleIdle}
		h.integ = nil
		h.controlChanged()
		h.toast("error", "键鼠捕获启动失败: "+err.Error())
		return
	}
	h.captureCh = ch
	h.captureStop = stop
	h.ctlDone = make(chan struct{})
	done := h.ctlDone
	go h.captureLoop(s, ch, done)
	h.controlChanged()
	h.toast("info", "正在控制 "+s.PeerName()+"（热键或推回边缘可结束）")
}

// onCtlDeny 我方发起的控制被拒绝。
func (h *Hub) onCtlDeny(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role == roleController && h.ctl.PeerID == s.peer.ID {
		h.ctl = ControlState{Role: roleIdle}
		h.integ = nil
		h.controlChanged()
		h.toast("warn", s.PeerName()+" 拒绝了控制请求")
	}
}

// moveFrameInterval 是鼠标移动的合帧窗口：窗口内的多次移动只发最新位置，
// 高报点率鼠标（125-1000Hz）不会打爆网络与对端注入。
const moveFrameInterval = 8 * time.Millisecond

// captureLoop 捕获事件 → 转发；移动合帧，按键/按钮/滚轮立即发（先冲刷 pending
// 移动保证顺序）；ctlDone 关闭或控制态丢失即退出。
func (h *Hub) captureLoop(s *Session, ch <-chan InputEvent, done <-chan struct{}) {
	pressed := NewPressedKeys()
	ticker := time.NewTicker(moveFrameInterval)
	defer ticker.Stop()
	var nx, ny uint32
	var dirty bool
	flush := func() {
		if !dirty {
			return
		}
		dirty = false
		_ = s.sendInput(InputEvent{Kind: InputMouseMove, MoveX: int32(nx), MoveY: int32(ny)})
	}
	for {
		var ev InputEvent
		var ok bool
		select {
		case ev, ok = <-ch:
			if !ok {
				return
			}
		case <-done:
			return
		case <-ticker.C:
			flush()
			continue
		}
		h.mu.Lock()
		active := h.ctl.Role == roleController && h.ctl.PeerID == s.peer.ID && h.integ != nil
		if !active {
			h.mu.Unlock()
			return
		}
		switch ev.Kind {
		case InputMouseMove:
			pos := h.integ.Move(ev.MoveX, ev.MoveY)
			nx, ny = Normalize(pos, h.integ.Bounds)
			dirty = true
			h.mu.Unlock()
		case InputKey:
			pressed.Observe(ev.VK, ev.Down)
			h.mu.Unlock()
			flush()
			_ = s.sendInput(ev)
		default:
			h.mu.Unlock()
			flush()
			_ = s.sendInput(ev)
		}
	}
}

// ReleaseControl 被控端拿回控制（或控制端主动放手，视角色而定）。
func (h *Hub) ReleaseControl(yRatio float64) {
	h.mu.Lock()
	if h.ctl.Role == roleIdle {
		h.mu.Unlock()
		return
	}
	s := h.sessions[h.ctl.PeerID]
	h.mu.Unlock()
	if s == nil {
		h.endControlLocal()
		return
	}
	if yRatio < 0 {
		_, y := h.platform.CursorPos()
		yRatio = cursorYRatio(h.platform.ScreenBounds(), y)
	}
	_ = s.sendJSON(wireMsg{T: msgCtlRelease, Y: yRatio})
	// 等对端 ok 再本地清理，避免两端光标跳动不一致；超时兜底强制清理。
	go func() {
		select {
		case <-time.After(1500 * time.Millisecond):
			h.endControlLocal()
		case <-s.done:
		}
	}()
}

// onCtlChain 链式切换：被控端把输入焦点导向另一台（控制器改链目标）。
func (h *Hub) onCtlChain(s *Session, m wireMsg) {
	h.mu.Lock()
	if h.ctl.Role != roleController || h.ctl.PeerID != s.peer.ID {
		h.mu.Unlock()
		return
	}
	targetID := m.FID
	target := h.sessions[targetID]
	h.mu.Unlock()
	if target == nil || !target.Alive() {
		_ = s.sendJSON(wireMsg{T: msgCtlDeny})
		return
	}
	if _, ok := h.cfg.Peer(targetID); !ok {
		_ = s.sendJSON(wireMsg{T: msgCtlDeny})
		return
	}
	// 通知旧被控端复位，再本地清理；随后接上新目标（捕获短暂重建，v1 允许微小空窗）。
	_ = s.sendJSON(wireMsg{T: msgCtlRelease, Y: 0})
	h.endControlLocal()
	if err := h.TakeControl(targetID, m.Y); err != nil {
		_ = s.sendJSON(wireMsg{T: msgCtlDeny})
		h.toast("warn", "链式切换失败: "+err.Error())
	} else {
		h.toast("info", "链式切换 → "+target.PeerName())
	}
}

// onCtlRelease 对端结束了控制。
func (h *Hub) onCtlRelease(s *Session, m wireMsg) {
	h.mu.Lock()
	if h.ctl.PeerID != s.peer.ID || h.ctl.Role == roleIdle {
		h.mu.Unlock()
		return
	}
	role := h.ctl.Role
	h.cleanupControlLocked()
	h.controlChanged()
	h.mu.Unlock()
	_ = s.sendJSON(wireMsg{T: msgCtlReleaseOK})
	if role == roleController {
		h.toast("info", "已停止控制 "+s.PeerName())
	} else {
		h.toast("info", s.PeerName()+" 已交还控制")
	}
}

// onCtlReleaseOK 对端确认结束，本地收尾。
func (h *Hub) onCtlReleaseOK(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.PeerID != s.peer.ID || h.ctl.Role == roleIdle {
		return
	}
	h.cleanupControlLocked()
	h.controlChanged()
}

// endControlLocal 强制本地收尾（超时/断线兜底）。
func (h *Hub) endControlLocal() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role == roleIdle {
		return
	}
	h.cleanupControlLocked()
	h.controlChanged()
}

// cleanupControlLocked 复位控制状态：停捕获、停注入、还光标、清按键。
// 注意：channel 由平台的 stop() 负责收尾，这里只关自己的退出信号，避免重复 close。
func (h *Hub) cleanupControlLocked() {
	if h.captureStop != nil {
		stop := h.captureStop
		h.captureStop = nil
		stop()
	}
	if h.ctlDone != nil {
		close(h.ctlDone)
		h.ctlDone = nil
	}
	h.captureCh = nil
	if h.injector != nil {
		h.injector.ReleaseAll()
		h.injector = nil
	}
	h.injKeys = nil
	h.injButtons = map[MouseButton]bool{}
	if h.ctl.Role == roleController {
		h.platform.SetCursor(h.savedX, h.savedY)
	}
	h.ctl = ControlState{Role: roleIdle}
	h.integ = nil
}

// handleRemoteInput 被控端：注入对端转发来的输入。
func (h *Hub) handleRemoteInput(s *Session, ev InputEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctl.Role != roleControlled || h.ctl.PeerID != s.peer.ID || h.injector == nil {
		return
	}
	switch ev.Kind {
	case InputMouseMove:
		h.injector.InjectMove(uint32(ev.MoveX), uint32(ev.MoveY))
	case InputButton:
		h.injButtons[ev.Button] = ev.Down
		h.injector.InjectButton(ev.Button, ev.Down)
	case InputWheel:
		h.injector.InjectWheel(ev.Wheel)
	case InputKey:
		h.injKeys.Observe(ev.VK, ev.Down)
		h.injector.InjectKey(ev.VK, ev.Scan, ev.Ext, ev.Down)
	}
}

// watchHotkey 消费平台热键信号。
func (h *Hub) watchHotkey(ctx context.Context) {
	if h.platform.Hotkey == nil {
		return
	}
	for {
		select {
		case <-h.platform.Hotkey:
			h.hotkeyPressed()
		case <-ctx.Done():
			return
		}
	}
}

// watchEscape 被控端 Esc 快捷交还：被控制时按 Esc 立即拿回键鼠。
func (h *Hub) watchEscape(ctx context.Context) {
	if h.platform.Escape == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.platform.Escape:
			h.mu.Lock()
			role := h.ctl.Role
			peer := h.ctl.Peer
			h.mu.Unlock()
			if role == roleControlled {
				h.ReleaseControl(-1)
				h.toast("info", "已按 Esc 交还控制"+controlPeerSuffix(peer))
			}
		}
	}
}

func controlPeerSuffix(peer string) string {
	if peer == "" {
		return ""
	}
	return "（" + peer + "）"
}

// hotkeyPressed 热键：空闲→控制对端；控制中→结束；被控→拿回。
func (h *Hub) hotkeyPressed() {
	h.mu.Lock()
	role := h.ctl.Role
	h.mu.Unlock()
	switch role {
	case roleIdle:
		// 优先有方位的对端，其次任一在线对端。
		for _, p := range h.cfg.Peers {
			if p.Enabled && p.Position != "" {
				if err := h.TakeControl(p.ID, -1); err == nil {
					return
				}
			}
		}
		for _, p := range h.cfg.Peers {
			if p.Enabled {
				if err := h.TakeControl(p.ID, -1); err == nil {
					return
				}
			}
		}
		h.toast("warn", "热键：没有可控制的配对设备")
	case roleController:
		h.stopControlling()
	case roleControlled:
		h.ReleaseControl(-1)
	}
}

// stopControlling 控制端主动结束。
func (h *Hub) stopControlling() {
	h.mu.Lock()
	peerID := h.ctl.PeerID
	s := h.sessions[peerID]
	h.mu.Unlock()
	if s != nil {
		_ = s.sendJSON(wireMsg{T: msgCtlRelease, Y: 0})
	}
	h.endControlLocal()
	h.toast("info", "已结束键鼠共享")
}

// cursorYRatio 光标纵坐标在屏幕内的比例。
func cursorYRatio(b ScreenBounds, y int32) float64 {
	if b.H <= 1 {
		return 0.5
	}
	r := (float64(y) - float64(b.Y)) / float64(b.H)
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	return r
}
