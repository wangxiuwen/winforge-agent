package desk

import (
	"encoding/json"
	"errors"
	"net"
	"time"
)

// 配对流程（明文通道，靠配对码认证 ECDH）：
//   发起方 A                          接受方 B
//   pair_request{id,name,epk,nonce} ->
//                                   <- pair_ack{id,name,epk,nonce}
//   双方各自算 code 并在 UI 显示
//   用户点确认 -> pair_confirm_a{proof}
//                                   用户点确认 -> pair_confirm_b{proof}
//   A 校验 B 的证明并保存，发 pair_done
//                                   B 收到 pair_done 保存
// 任一方未点确认前，另一方的确认只会等待（90s 超时）。

type pairFrame struct {
	T     string `json:"t"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	EPK   string `json:"epk"`
	Nonce string `json:"nonce"`
	Proof string `json:"proof,omitempty"`
	Code  string `json:"code,omitempty"`
}

// pairOutcome 传递配对结果给 Hub。
type pairOutcome struct {
	peer Peer
}

// runPairingInitiator 是发起方：拨号 → 展示配对码 → 双确认 → 保存。
func runPairingInitiator(h *Hub, addr string) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		h.emitPairEvent("failed", "", "无法连接 "+addr+": "+err.Error())
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(pairingTimeout))

	kp, err := newEphKeyPair()
	if err != nil {
		h.emitPairEvent("failed", "", err.Error())
		return
	}
	nonce := randomBytes(16)
	req := pairFrame{T: msgPairRequest, ID: h.cfg.ID, Name: h.cfg.Name, EPK: b64(kp.pub), Nonce: b64(nonce)}
	if err := writeJSONFrame(conn, req); err != nil {
		h.emitPairEvent("failed", "", "发送配对请求失败: "+err.Error())
		return
	}
	var ack pairFrame
	if err := readJSONFrame(conn, &ack); err != nil || ack.T != msgPairAck {
		h.emitPairEvent("failed", "", "对端未响应配对请求")
		return
	}
	secret, code, err := computePairingSecret(kp, kp.pub, nonce, unb64(ack.EPK), unb64(ack.Nonce), true)
	if err != nil {
		h.emitPairEvent("failed", "", "配对密钥计算失败: "+err.Error())
		return
	}
	h.setPairOutCode(code)
	h.emitPairEvent("code", code, "等待对方在 "+ack.Name+" 上确认")
	// 等待本机用户点“确认配对”；取消则礼貌告知对端。
	select {
	case ok := <-h.initiatorConfirmChan():
		if !ok {
			_ = writeJSONFrame(conn, pairFrame{T: msgPairReject})
			h.emitPairEvent("failed", code, "已取消配对")
			return
		}
	case <-time.After(pairingTimeout):
		h.emitPairEvent("failed", code, "等待本机确认超时")
		return
	}
	if err := writeJSONFrame(conn, pairFrame{T: msgPairConfirmA, Proof: b64(pairProof(secret, "A"))}); err != nil {
		h.emitPairEvent("failed", code, "发送确认失败: "+err.Error())
		return
	}
	var done pairFrame
	if err := readJSONFrame(conn, &done); err != nil {
		h.emitPairEvent("failed", code, "对方未完成确认")
		return
	}
	switch done.T {
	case msgPairConfirmB:
		if err := verifyPairProof(secret, "B", unb64(done.Proof)); err != nil {
			h.emitPairEvent("failed", code, "对方证明校验失败（配对码不一致？）")
			return
		}
	case msgPairReject:
		h.emitPairEvent("failed", code, "对方拒绝了配对")
		return
	default:
		h.emitPairEvent("failed", code, "对端响应异常")
		return
	}
	psk, err := pairingPSK(secret)
	if err != nil {
		h.emitPairEvent("failed", code, err.Error())
		return
	}
	// 对端身份以 pair_ack 声明的 ID/Name 为准（confirm 帧不带身份字段）。
	if err := h.savePairedPeer(ack.ID, ack.Name, psk, addr); err != nil {
		h.emitPairEvent("failed", code, "保存失败: "+err.Error())
		return
	}
	_ = writeJSONFrame(conn, pairFrame{T: msgPairDone})
	h.emitPairEvent("done", code, "已与 "+ack.Name+" 配对")
}

// runPairingResponder 是接受方：请求已由 accept 循环读出 → 弹确认（含配对码）→ 双确认 → 保存。
func runPairingResponder(h *Hub, conn net.Conn, req pairFrame) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(pairingTimeout))
	kp, err := newEphKeyPair()
	if err != nil {
		return
	}
	nonce := randomBytes(16)
	ack := pairFrame{T: msgPairAck, ID: h.cfg.ID, Name: h.cfg.Name, EPK: b64(kp.pub), Nonce: b64(nonce)}
	if err := writeJSONFrame(conn, ack); err != nil {
		return
	}
	secret, code, err := computePairingSecret(kp, kp.pub, nonce, unb64(req.EPK), unb64(req.Nonce), false)
	if err != nil {
		return
	}
	reqID := h.registerPairRequest(req.ID, req.Name, code)
	defer h.clearPairRequest(reqID)
	localCh := h.pairRequestConfirmChan(reqID)

	// 读协程把后续帧送进通道，主循环同时等：本机确认、对端帧、超时。
	frames := make(chan pairFrame, 8)
	readErr := make(chan error, 1)
	go func() {
		for {
			var f pairFrame
			if err := readJSONFrame(conn, &f); err != nil {
				readErr <- err
				close(frames)
				return
			}
			frames <- f
		}
	}()

	confirmedLocal, confirmedRemote := false, false
	deadline := time.After(pairingTimeout)
	for !(confirmedLocal && confirmedRemote) {
		select {
		case ok := <-localCh:
			if !ok {
				_ = writeJSONFrame(conn, pairFrame{T: msgPairReject})
				h.emitPairEvent("failed", code, "已拒绝配对")
				return
			}
			confirmedLocal = true
			if err := writeJSONFrame(conn, pairFrame{T: msgPairConfirmB, Proof: b64(pairProof(secret, "B"))}); err != nil {
				h.emitPairEvent("failed", code, "发送确认失败")
				return
			}
		case f, ok := <-frames:
			if !ok {
				h.emitPairEvent("failed", code, "连接中断")
				return
			}
			switch f.T {
			case msgPairConfirmA:
				if err := verifyPairProof(secret, "A", unb64(f.Proof)); err != nil {
					h.emitPairEvent("failed", code, "对方证明校验失败（配对码不一致？）")
					return
				}
				confirmedRemote = true
			case msgPairReject:
				h.emitPairEvent("failed", code, "对方取消了配对")
				return
			}
		case <-deadline:
			h.emitPairEvent("failed", code, "配对超时")
			return
		}
	}
	psk, err := pairingPSK(secret)
	if err != nil {
		return
	}
	// 应答方不知道发起方的控制端口（连接来自临时端口），地址留给发现服务补。
	if err := h.savePairedPeer(req.ID, req.Name, psk, ""); err != nil {
		h.emitPairEvent("failed", code, "保存失败: "+err.Error())
		return
	}
	_ = writeJSONFrame(conn, pairFrame{T: msgPairDone})
	h.emitPairEvent("done", code, "已与 "+req.Name+" 配对")
}

// computePairingSecret 计算 ECDH 并派生配对密钥与配对码。
func computePairingSecret(kp *ephKeyPair, selfPub, selfNonce, peerPub, peerNonce []byte, initiator bool) (secret []byte, code string, err error) {
	if len(peerPub) != 32 || len(peerNonce) != 16 {
		return nil, "", errBadPubKey
	}
	var dhSecret []byte
	dhSecret, err = kp.dh(peerPub)
	if err != nil {
		return nil, "", err
	}
	if initiator {
		secret, err = pairingSecret(dhSecret, selfNonce, peerNonce)
	} else {
		secret, err = pairingSecret(dhSecret, peerNonce, selfNonce)
	}
	if err != nil {
		return nil, "", err
	}
	return secret, pairCode(secret), nil
}

// writeJSONFrame / readJSONFrame 是明文阶段帧封装。
func writeJSONFrame(conn net.Conn, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(conn, FrameJSON, b)
}

func readJSONFrame(conn net.Conn, v interface{}) error {
	typ, payload, err := readFrame(conn)
	if err != nil {
		return err
	}
	if typ != FrameJSON {
		return errors.New("配对阶段出现非 JSON 帧")
	}
	return json.Unmarshal(payload, v)
}
