package desk

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

// 这一份是 UI（Wails / 控制台）可调用的命令面 + 事件面。

var (
	errNoPairRequest = errors.New("没有这个配对请求")
	errBadChat       = errors.New("消息为空或超长")
	errBadPath       = errors.New("路径不在允许的目录内")
)

// PeerView 是 UI 里的对端视图。
type PeerView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	Enabled  bool   `json:"enabled"`
	Online   bool   `json:"online"`
	Addr     string `json:"addr"`
}

// Snapshot 是 UI 初始渲染用的完整状态。
func (h *Hub) Snapshot() map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	allPeers := h.cfg.PeersSnapshot()
	peers := make([]PeerView, 0, len(allPeers))
	for _, p := range allPeers {
		_, online := h.sessions[p.ID]
		peers = append(peers, PeerView{ID: p.ID, Name: p.Name, Position: p.Position,
			Enabled: p.Enabled, Online: online, Addr: p.Addr})
	}
	var presence []Presence
	if h.disc != nil {
		presence = h.disc.List()
	}
	out := map[string]interface{}{
		"self": map[string]interface{}{
			"id": h.cfg.ID, "name": h.cfg.Name, "hotkey": h.cfg.Hotkey,
			"port": h.ControlPort(),
		},
		"peers":     peers,
		"presence":  presence,
		"control":   h.ctl,
		"share":     h.cfg.ShareEnabledV(),
		"downloads": h.cfg.DownloadsDir,
		"transfers": h.transferHistory(),
	}
	if h.startupWarn != "" {
		out["startupWarning"] = h.startupWarn
	}
	if req := h.currentPairRequest(); req != nil {
		out["pairRequest"] = req
	}
	if h.pairOut != nil {
		out["pairOut"] = map[string]string{"addr": h.pairOut.Addr, "code": h.pairOut.Code}
	}
	return out
}

func (h *Hub) currentPairRequest() map[string]string {
	for _, r := range h.pairReqs {
		return map[string]string{"reqID": r.ReqID, "name": r.Name, "code": r.Code}
	}
	return nil
}

// emitState 在锁外发出状态快照。
func (h *Hub) emitState() {
	h.emit("desk:state", h.Snapshot())
}

// ---- 配对 ----

// addrHostRe 允许 IPv4/IPv6 字面量与常见主机名字符，拒绝空格等明显垃圾。
var addrHostRe = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// PairStart 发起配对（地址形如 192.168.1.5:41831）。
func (h *Hub) PairStart(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("地址格式应为 IP:端口: %w", err)
	}
	if !addrHostRe.MatchString(host) {
		return fmt.Errorf("地址含非法字符: %q", host)
	}
	h.mu.Lock()
	if h.pairOut != nil {
		h.mu.Unlock()
		return errors.New("已有进行中的配对")
	}
	chn := make(chan bool, 1)
	h.pairOut = &pendingPairOut{Addr: addr, chn: chn, open: true}
	h.mu.Unlock()
	go func() {
		runPairingInitiator(h, addr)
		h.mu.Lock()
		h.pairOut = nil
		h.mu.Unlock()
		h.emitState()
	}()
	h.emitState()
	return nil
}

// PairConfirmOut 发起方用户点了“确认配对/取消”。
func (h *Hub) PairConfirmOut(ok bool) {
	h.mu.Lock()
	p := h.pairOut
	h.mu.Unlock()
	if p != nil && p.chn != nil {
		select {
		case p.chn <- ok:
		default:
		}
	}
}

// initiatorConfirmChan 发起方等待本机确认。
func (h *Hub) initiatorConfirmChan() chan bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pairOut == nil {
		ch := make(chan bool, 1)
		ch <- false
		return ch
	}
	return h.pairOut.chn
}

// setPairOutCode 回写发起方的配对码（收到对端 ack 后）。
func (h *Hub) setPairOutCode(code string) {
	h.mu.Lock()
	if h.pairOut != nil {
		h.pairOut.Code = code
	}
	h.mu.Unlock()
	h.emitState()
}

// registerPairRequest 登记收到的配对请求。
func (h *Hub) registerPairRequest(id, name, code string) string {
	reqID := b64(randomBytes(8))
	h.mu.Lock()
	h.pairReqs[reqID] = &pendingPairIn{ReqID: reqID, ID: id, Name: name, Code: code, ch: make(chan bool, 2)}
	h.mu.Unlock()
	// request 态必须带 reqID：UI 确认按钮靠它回 PairRespond。
	// 曾漏带导致 UI 点"确认配对码一致"静默无效（R54 修复）。
	h.emit("desk:pair", map[string]string{"state": "request", "code": code,
		"message": name + " 请求配对", "reqID": reqID})
	h.emitState()
	return reqID
}

// pairRequestConfirmChan 返回指定请求的确认通道。
func (h *Hub) pairRequestConfirmChan(reqID string) chan bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.pairReqs[reqID]; r != nil {
		return r.ch
	}
	ch := make(chan bool, 1)
	ch <- false
	return ch
}

func (h *Hub) clearPairRequest(reqID string) {
	h.mu.Lock()
	delete(h.pairReqs, reqID)
	h.mu.Unlock()
	h.emitState()
}

// PairRespond 本机对收到的配对请求表态。
func (h *Hub) PairRespond(reqID string, ok bool) {
	h.mu.Lock()
	r := h.pairReqs[reqID]
	h.mu.Unlock()
	if r != nil && r.ch != nil {
		select {
		case r.ch <- ok:
		default:
		}
	}
}

// savePairedPeer 配对成功后落盘。
func (h *Hub) savePairedPeer(id, name string, psk []byte, addr string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	peer := Peer{ID: id, Name: sanitizeDisplayName(name), PSK: b64(psk),
		Addr: addr, Enabled: true, AddedAt: time.Now().Unix()}
	if old, ok := h.cfg.Peer(id); ok {
		peer.Position = old.Position
	}
	if err := h.cfg.UpPeer(h.cfgPath, peer); err != nil {
		return err
	}
	h.wakeSessions()
	return nil
}

// emitPairEvent 配对过程事件。
func (h *Hub) emitPairEvent(state, code, message string) {
	h.emit("desk:pair", map[string]string{"state": state, "code": code, "message": message})
	if state == "done" || state == "failed" {
		level := "info"
		if state == "failed" {
			level = "warn"
		}
		h.toast(level, message)
	}
}

// ---- 对端管理 ----

// PeerPosition 设置对端方位。
func (h *Hub) PeerPosition(id, pos string) error {
	h.mu.Lock()
	err := h.cfg.SetPeerPosition(h.cfgPath, id, pos)
	h.mu.Unlock()
	h.emitState()
	return err
}

// PeerToggle 启用/停用对端。
func (h *Hub) PeerToggle(id string, on bool) error {
	h.mu.Lock()
	err := h.cfg.SetPeerEnabled(h.cfgPath, id, on)
	h.mu.Unlock()
	h.emitState()
	return err
}

// PeerRemove 删除对端（同时断开会话）。
func (h *Hub) PeerRemove(id string) error {
	h.mu.Lock()
	s := h.sessions[id]
	err := h.cfg.RemovePeer(h.cfgPath, id)
	h.mu.Unlock()
	if s != nil {
		s.close()
	}
	h.DeleteChatHistory(id)
	h.emitState()
	return err
}

// ---- 消息与文件 ----

// SendChat 发消息给对端。
func (h *Hub) SendChat(peerID, text string) error {
	if text == "" || len(text) > maxChatLength {
		return errBadChat
	}
	h.mu.Lock()
	s := h.sessions[peerID]
	h.mu.Unlock()
	if s == nil || !s.Alive() {
		return errNotConnected
	}
	if err := s.sendJSON(wireMsg{T: msgChat, Text: text, Ts: time.Now().UnixMilli()}); err != nil {
		return err
	}
	h.recordChat(peerID, ChatMsg{Text: text, Ts: time.Now().UnixMilli(), Outgoing: true})
	h.emit("desk:chat", map[string]interface{}{
		"from": "我", "peer": peerID, "text": text,
		"ts": time.Now().UnixMilli(), "outgoing": true,
	})
	return nil
}

// SendFile 发送本地文件。
func (h *Hub) SendFile(peerID, path string) error {
	sf, err := openSenderFile(path, h.cfg.MaxFileBytesV())
	if err != nil {
		return err
	}
	h.mu.Lock()
	s := h.sessions[peerID]
	if s == nil || !s.Alive() {
		h.mu.Unlock()
		_ = sf.Close()
		return errNotConnected
	}
	id := newTransferID()
	t := &outgoingTransfer{ID: id, Peer: peerID, Name: filepath.Base(path),
		Size: sf.size, SHA: sf.sha, State: "offering", Path: path, file: sf}
	h.out[id] = t
	h.mu.Unlock()
	if err := s.sendJSON(wireMsg{T: msgFileOffer, FID: b64(id[:]), Name: t.Name, Size: t.Size, SHA: t.SHA}); err != nil {
		h.failOutgoing(t, err.Error())
		return err
	}
	h.emit("desk:transfer", map[string]interface{}{
		"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": t.Name,
		"dir": "out", "state": "offering", "recv": 0, "size": t.Size,
	})
	return nil
}

// TransferRespond 接收方对收到的文件表态。
func (h *Hub) TransferRespond(fidHex string, ok bool) {
	var id [16]byte
	b, _ := hex.DecodeString(fidHex)
	if len(b) != 16 {
		return
	}
	copy(id[:], b)
	h.mu.Lock()
	t := h.in[id]
	if t == nil || t.State != "offer" {
		h.mu.Unlock()
		return
	}
	peerID := t.Peer
	h.mu.Unlock()
	s := func() *Session {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.sessions[peerID]
	}()
	if s == nil {
		return
	}
	if !ok {
		_ = s.sendJSON(wireMsg{T: msgFileReject, FID: b64(id[:])})
		h.mu.Lock()
		delete(h.in, id)
		h.mu.Unlock()
		h.emit("desk:transfer", map[string]interface{}{
			"id": fidHex, "dir": "in", "state": "rejected",
		})
		return
	}
	f, err := newIncomingFile(h.cfg.DownloadsDir, t.Name, t.Size, id)
	if err != nil {
		_ = s.sendJSON(wireMsg{T: msgFileReject, FID: b64(id[:])})
		h.toast("error", "无法保存文件 "+t.Name+": "+err.Error())
		return
	}
	h.mu.Lock()
	t.file = f
	t.State = "receiving"
	t.Path = f.path
	h.mu.Unlock()
	_ = s.sendJSON(wireMsg{T: msgFileAccept, FID: b64(id[:])})
	h.emit("desk:transfer", map[string]interface{}{
		"id": fidHex, "dir": "in", "state": "receiving", "recv": 0, "size": t.Size,
	})
}

// ---- 其他命令 ----

// ToggleShare 共享总开关。
func (h *Hub) ToggleShare(on bool) {
	err := h.cfg.SetShareEnabled(h.cfgPath, on)
	h.mu.Lock()
	active := h.ctl.Role != roleIdle
	h.mu.Unlock()
	if err != nil {
		h.toast("error", "保存配置失败: "+err.Error())
	}
	if !on && active {
		// 关共享时立即终止进行中的控制（锁外操作，内部各自拿锁）。
		h.stopControlling()
		h.endControlLocal()
	}
	h.emitState()
}

// OpenPath 打开下载目录或其中的文件（只允许下载目录内）。
func (h *Hub) OpenPath(path string) error {
	if path == "" {
		path = h.cfg.DownloadsDir
	}
	if !withinDir(h.cfg.DownloadsDir, path) && path != h.cfg.DownloadsDir {
		return errBadPath
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
