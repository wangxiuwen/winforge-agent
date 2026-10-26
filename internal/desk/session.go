package desk

import (
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Session 是与一台已配对设备的持久加密连接：
//   明文握手（hello/hello_ack/hello_ready，HMAC 证明持有 PSK）
//   → 之后的每一帧 AES-256-GCM 密封，方向独立计数器。
// 断线由 runSessions 按 3s 起步的退避自动重连。

// wireMsg 是会话 JSON 消息的统一形状，字段按需 omitempty。
type wireMsg struct {
	T string `json:"t"`

	// state / 握手与文件共用：设备名或文件名（不同消息互不重叠）
	Name         string `json:"name,omitempty"`
	ShareEnabled *bool  `json:"share_enabled,omitempty"`
	Controlling  string `json:"controlling,omitempty"` // 对方正控制的对端 ID，空为空闲
	// Port 是对端的控制监听端口（state 消息）：应答方借此学到主动方地址，
	// discovery 不可用时也能主动回连。
	Port int `json:"port,omitempty"`

	// chat
	Text string `json:"text,omitempty"`
	Ts   int64  `json:"ts,omitempty"`

	// 文件
	FID  string `json:"fid,omitempty"`
	Size int64  `json:"size,omitempty"`
	SHA  string `json:"sha,omitempty"`

	// 控制
	Y float64 `json:"y,omitempty"`
}

// inputEventFrame 是 FrameInput 的二进制载荷（低频高吞吐路径，不走 JSON）。
// [1B kind][8B 保留对齐][按 kind 的定长体]，kind 取值复用 InputKind。
func inputEventFrame(ev InputEvent) []byte {
	out := make([]byte, 16)
	out[0] = byte(ev.Kind)
	switch ev.Kind {
	case InputMouseMove:
		putI32(out[8:], ev.MoveX)
		putI32(out[12:], ev.MoveY)
	case InputButton:
		out[8] = byte(ev.Button)
		if ev.Down {
			out[9] = 1
		}
	case InputWheel:
		putI32(out[8:], ev.Wheel)
	case InputKey:
		putU32(out[8:], ev.VK)
		putU32(out[12:], ev.Scan)
		if ev.Down {
			out[9] |= 0x01
		}
		if ev.Ext {
			out[9] |= 0x02
		}
	}
	return out
}

func parseInputEvent(b []byte) (InputEvent, error) {
	if len(b) < 16 {
		return InputEvent{}, errors.New("输入帧过短")
	}
	ev := InputEvent{Kind: InputKind(b[0])}
	switch ev.Kind {
	case InputMouseMove:
		ev.MoveX = getI32(b[8:])
		ev.MoveY = getI32(b[12:])
	case InputButton:
		ev.Button = MouseButton(b[8])
		ev.Down = b[9]&0x01 != 0
	case InputWheel:
		ev.Wheel = getI32(b[8:])
	case InputKey:
		ev.VK = getU32(b[8:])
		ev.Scan = getU32(b[12:])
		ev.Down = b[9]&0x01 != 0
		ev.Ext = b[9]&0x02 != 0
	}
	return ev, nil
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func getU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func putI32(b []byte, v int32) { putU32(b, uint32(v)) }

func getI32(b []byte) int32 { return int32(getU32(b)) }

// Session 一条与对端的加密会话。
type Session struct {
	hub       *Hub
	peer      Peer
	conn      net.Conn
	keys      *sessionKeys
	// initiatorID 是拨号方的 ID：主动连接=本机 ID，被动接受=对端 ID。
	// 双端并发拨号时用它做唯一性仲裁（保留 initiatorID 较小者）。
	initiatorID string
	sendMu    sync.Mutex
	peerName  string
	peerShare bool
	peerCtl   string // 对端正在控制的第三方
	lastRecv  atomic.Int64
	closed    atomic.Bool
	done      chan struct{}
}

// PeerID 返回对端 ID。
func (s *Session) PeerID() string { return s.peer.ID }

// Alive 报告会话是否仍可收发（重连切换窗口期内旧会话已死但未及清理）。
func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// PeerName 返回对端最新名字。
func (s *Session) PeerName() string {
	if s.peerName != "" {
		return s.peerName
	}
	return s.peer.Name
}

func (s *Session) close() {
	if s.closed.CompareAndSwap(false, true) {
		_ = s.conn.Close()
		close(s.done)
	}
}

// sendJSON 发一条密封 JSON 消息。外层 [type][len] 帧头不加密（只泄露长度与类型），
// 载荷整体走 AES-GCM。
func (s *Session) sendJSON(m wireMsg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return writeFrame(s.conn, FrameJSON, s.keys.seal(b))
}

// sendChunk 发一个密封文件块。
func (s *Session) sendChunk(id [16]byte, seq uint32, data []byte) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return writeFrame(s.conn, FrameChunk, s.keys.seal(chunkPayload(id, seq, data)))
}

// sendInput 发一个密封输入事件帧。
func (s *Session) sendInput(ev InputEvent) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return writeFrame(s.conn, FrameInput, s.keys.seal(inputEventFrame(ev)))
}

// handshake 执行握手并完成密钥派生。initiator=true 表示本端主动连接。
// 返回对端名（握手中的声明）。
func (s *Session) handshake(psk []byte, initiator bool) error {
	_ = s.conn.SetDeadline(time.Now().Add(handshakeLimit))
	kp, err := newEphKeyPair()
	if err != nil {
		return err
	}
	nonce := randomBytes(16)
	hello := struct {
		wireMsg
		ID    string `json:"id"`
		EPK   string `json:"epk"`
		Nonce string `json:"nonce"`
	}{wireMsg: wireMsg{T: msgHello, Name: s.hub.cfg.Name}, ID: s.hub.cfg.ID, EPK: b64(kp.pub), Nonce: b64(nonce)}
	if err := writeJSONFrame(s.conn, hello); err != nil {
		return err
	}
	var ack struct {
		wireMsg
		ID    string `json:"id"`
		EPK   string `json:"epk"`
		Nonce string `json:"nonce"`
		Proof string `json:"proof"`
	}
	if err := readJSONFrame(s.conn, &ack); err != nil || ack.T != msgHelloAck {
		return errors.New("握手响应异常")
	}
	peerPub, peerNonce := unb64(ack.EPK), unb64(ack.Nonce)
	if len(peerPub) != 32 || len(peerNonce) != 16 {
		return errBadPubKey
	}
	dhSecret, err := kp.dh(peerPub)
	if err != nil {
		return err
	}
	var idA, nonceA, epkA, idB, nonceB, epkB []byte
	if initiator {
		idA, nonceA, epkA = []byte(s.hub.cfg.ID), nonce, kp.pub
		idB, nonceB, epkB = []byte(ack.ID), peerNonce, peerPub
	} else {
		idA, nonceA, epkA = []byte(ack.ID), peerNonce, peerPub
		idB, nonceB, epkB = []byte(s.hub.cfg.ID), nonce, kp.pub
	}
	ctx := helloContext(idA, nonceA, epkA, idB, nonceB, epkB)
	want := helloProof(psk, ctx)
	got := unb64(ack.Proof)
	if len(got) != len(want) || string(got) != string(want) {
		return errBadProof
	}
	if s.keys, err = deriveSessionKeys(psk, dhSecret, nonceA, nonceB, initiator); err != nil {
		return err
	}
	ready := struct {
		wireMsg
		Proof string `json:"proof"`
	}{wireMsg: wireMsg{T: msgHelloReady}, Proof: b64(helloProof(psk, ctx))}
	if err := writeJSONFrame(s.conn, ready); err != nil {
		return err
	}
	if ack.ID != s.peer.ID {
		return errors.New("对端 ID 与配对记录不符")
	}
	return nil
}

// handshakeFrom 是被动方握手：发起方的 hello 已经由 accept 循环读出。
func (s *Session) handshakeFrom(hello pairFrame, psk []byte) error {
	_ = s.conn.SetDeadline(time.Now().Add(handshakeLimit))
	if len(unb64(hello.EPK)) != 32 || len(unb64(hello.Nonce)) != 16 {
		return errBadPubKey
	}
	kp, err := newEphKeyPair()
	if err != nil {
		return err
	}
	nonce := randomBytes(16)
	ack := struct {
		wireMsg
		ID    string `json:"id"`
		EPK   string `json:"epk"`
		Nonce string `json:"nonce"`
		Proof string `json:"proof"`
	}{wireMsg: wireMsg{T: msgHelloAck, Name: s.hub.cfg.Name}, ID: s.hub.cfg.ID,
		EPK: b64(kp.pub), Nonce: b64(nonce)}
	// 上下文与发起方对称：A=发起方（对端），B=本机。
	ctx := helloContext([]byte(hello.ID), unb64(hello.Nonce), unb64(hello.EPK),
		[]byte(s.hub.cfg.ID), nonce, kp.pub)
	ack.Proof = b64(helloProof(psk, ctx))
	if err := writeJSONFrame(s.conn, ack); err != nil {
		return err
	}
	var ready struct {
		wireMsg
		Proof string `json:"proof"`
	}
	if err := readJSONFrame(s.conn, &ready); err != nil || ready.T != msgHelloReady {
		return errors.New("握手未完成")
	}
	want := helloProof(psk, ctx)
	got := unb64(ready.Proof)
	if len(got) != len(want) || string(got) != string(want) {
		return errBadProof
	}
	dhSecret, err := kp.dh(unb64(hello.EPK))
	if err != nil {
		return err
	}
	if s.keys, err = deriveSessionKeys(psk, dhSecret, unb64(hello.Nonce), nonce, false); err != nil {
		return err
	}
	return nil
}

// serve 是会话主循环：读帧 → 分发；同时负责保活与超时。
func (s *Session) serve() {
	// 拦截 serve 内部 panic：会话异常时走正常关闭路径而不是带崩进程。
	defer safeCall(s.hub.log, "session", func() {})
	defer func() {
		s.close()
		s.hub.sessionClosed(s)
	}()
	go s.keepalive()
	sendState := func() {
		share := s.hub.cfg.ShareEnabled
		_ = s.sendJSON(wireMsg{T: msgState, Name: s.hub.cfg.Name, ShareEnabled: &share,
			Controlling: s.hub.controllingPeerID(), Port: s.hub.ControlPort()})
	}
	sendState()
	// 首条 state 与对端握手收尾存在竞态窗口，3s 后重发一次让对端的
	// 地址学习/共享状态同步更可靠（幂等消息，重复无害）。
	safeGo(s.hub.log, "state-resend", func() {
		select {
		case <-s.done:
			return
		case <-time.After(3 * time.Second):
		}
		if s.Alive() {
			sendState()
		}
	})
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(dropAfter))
		typ, sealed, err := readFrame(s.conn)
		if err != nil {
			return
		}
		s.lastRecv.Store(time.Now().Unix())
		plain, err := s.keys.open(sealed)
		if err != nil {
			// 解封失败意味着密钥失配或流量被篡改，必须断开。
			s.hub.log.Printf("会话 %s 帧解封失败: %v", s.peer.ID, err)
			return
		}
		switch typ {
		case FrameJSON:
			var m wireMsg
			if json.Unmarshal(plain, &m) != nil {
				return
			}
			if !s.hub.handleSessionMessage(s, m) {
				return
			}
		case FrameChunk:
			id, seq, data, err := parseChunk(plain)
			if err != nil {
				return
			}
			s.hub.handleChunk(s, id, seq, data)
		case FrameInput:
			ev, err := parseInputEvent(plain)
			if err != nil {
				return
			}
			s.hub.handleRemoteInput(s, ev)
		default:
			s.hub.log.Printf("会话 %s 出现未知帧型 %d，断开", s.peer.ID, typ)
			return
		}
	}
}

// keepalive 周期 ping；静默超过阈值则断开（由读侧 deadline 兜底）。
func (s *Session) keepalive() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if time.Since(time.Unix(s.lastRecv.Load(), 0)) > dropAfter {
				s.close()
				return
			}
			_ = s.sendJSON(wireMsg{T: msgPing})
		}
	}
}
