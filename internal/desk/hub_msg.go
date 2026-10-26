package desk

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// withinDir 判断 path 是否位于 dir 内（防穿越）。
func withinDir(dir, path string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if !strings.HasSuffix(absDir, string(filepath.Separator)) {
		absDir += string(filepath.Separator)
	}
	return strings.HasPrefix(absPath, absDir)
}

// removeFile 包装 os.Remove。
func removeFile(path string) error { return os.Remove(path) }

// handleSessionMessage 处理一条密封 JSON 消息；返回 false 表示应终止会话。
func (h *Hub) handleSessionMessage(s *Session, m wireMsg) bool {
	switch m.T {
	case msgPing:
		_ = s.sendJSON(wireMsg{T: msgPong})
	case msgPong:
	case msgState:
		if m.Name != "" {
			s.peerName = m.Name
		}
		if m.ShareEnabled != nil {
			s.peerShare = *m.ShareEnabled
		}
		s.peerCtl = m.Controlling
		h.emitState()
	case msgChat:
		if len(m.Text) == 0 || len(m.Text) > maxChatLength {
			return false
		}
		ts := m.Ts
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		h.recordChat(s.peer.ID, ChatMsg{Text: m.Text, Ts: ts, Outgoing: false})
		h.emit("desk:chat", map[string]interface{}{
			"from": s.PeerName(), "peer": s.peer.ID,
			"text": m.Text, "ts": ts, "outgoing": false,
		})
	case msgFileOffer:
		h.onFileOffer(s, m)
	case msgFileAccept:
		h.onFileAccept(s, m)
	case msgFileReject:
		h.onFileReject(s, m)
	case msgFileDone:
		h.onFileDone(s, m)
	case msgFileAbort:
		h.onFileAbort(s, m)
	case msgCtlTake:
		h.onCtlTake(s, m)
	case msgCtlTakeOK:
		h.onCtlTakeOK(s)
	case msgCtlDeny:
		h.onCtlDeny(s)
	case msgCtlChain:
		h.onCtlChain(s, m)
	case msgCtlRelease:
		h.onCtlRelease(s, m)
	case msgCtlReleaseOK:
		h.onCtlReleaseOK(s)
	default:
		// 未知消息忽略，保持向前兼容。
	}
	return true
}

// onFileOffer 收到文件发送请求。
func (h *Hub) onFileOffer(s *Session, m wireMsg) {
	if m.Name == "" || m.Size < 0 || m.Size > h.cfg.MaxFileBytes {
		_ = s.sendJSON(wireMsg{T: msgFileReject, FID: m.FID})
		return
	}
	var id [16]byte
	if b := unb64(m.FID); len(b) != 16 {
		_ = s.sendJSON(wireMsg{T: msgFileReject, FID: m.FID})
		return
	} else {
		copy(id[:], b)
	}
	name := sanitizeFileName(m.Name)
	if name == "" {
		_ = s.sendJSON(wireMsg{T: msgFileReject, FID: m.FID})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.in[id] = &incomingTransfer{ID: id, Peer: s.peer.ID, Name: name, Size: m.Size, SHA: m.SHA, State: "offer"}
	h.emit("desk:transfer", map[string]interface{}{
		"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": name,
		"dir": "in", "state": "offer", "recv": 0, "size": m.Size,
	})
	h.toast("info", s.PeerName()+" 想发送文件 "+name)
}

// onFileAccept 对端接受了我们发出的文件。
func (h *Hub) onFileAccept(s *Session, m wireMsg) {
	var id [16]byte
	b := unb64(m.FID)
	if len(b) != 16 {
		return
	}
	copy(id[:], b)
	h.mu.Lock()
	t := h.out[id]
	if t == nil || t.Peer != s.peer.ID || t.State != "offering" {
		h.mu.Unlock()
		return
	}
	t.State = "sending"
	h.mu.Unlock()
	go h.sendTransferChunks(s, t)
}

// sendTransferChunks 顺序发送文件块；TCP 背压即流控。
// 取消由 CancelTransfer 统一处理（置位 cancel 并同步清理），这里发现置位即退出。
func (h *Hub) sendTransferChunks(s *Session, t *outgoingTransfer) {
	buf := make([]byte, ChunkSize)
	seq := uint32(0)
	for {
		if t.cancel.Load() {
			return
		}
		n, err := t.file.f.Read(buf)
		if n > 0 {
			if err := s.sendChunk(t.ID, seq, buf[:n]); err != nil {
				h.failOutgoing(t, "发送失败: "+err.Error())
				return
			}
			t.Sent += int64(n)
			seq++
			if seq%16 == 0 {
				h.emit("desk:transfer", map[string]interface{}{
					"id": hex.EncodeToString(t.ID[:]), "peer": t.Peer, "name": t.Name,
					"dir": "out", "state": "sending", "recv": t.Sent, "size": t.Size,
				})
			}
		}
		if err != nil {
			if err := s.sendJSON(wireMsg{T: msgFileDone, FID: b64(t.ID[:]), SHA: t.SHA}); err != nil {
				h.failOutgoing(t, "发送失败: "+err.Error())
				return
			}
			h.emit("desk:transfer", map[string]interface{}{
				"id": hex.EncodeToString(t.ID[:]), "peer": t.Peer, "name": t.Name,
				"dir": "out", "state": "sent", "recv": t.Sent, "size": t.Size,
			})
			h.finishOutgoing(t)
			return
		}
	}
}

// finishOutgoing 发送完成后关闭句柄并清理临时上传文件。
func (h *Hub) finishOutgoing(t *outgoingTransfer) {
	h.mu.Lock()
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
	}
	delete(h.out, t.ID)
	h.mu.Unlock()
	h.cleanupTempUpload(t.Path)
}

func (h *Hub) failOutgoing(t *outgoingTransfer, reason string) {
	h.mu.Lock()
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
	}
	t.State = "failed"
	delete(h.out, t.ID)
	h.mu.Unlock()
	h.cleanupTempUpload(t.Path)
	h.toast("error", "文件 "+t.Name+" 发送失败："+reason)
}

// onFileReject 对端拒绝了我们的文件。
func (h *Hub) onFileReject(s *Session, m wireMsg) {
	var id [16]byte
	if b := unb64(m.FID); len(b) == 16 {
		copy(id[:], b)
		h.mu.Lock()
		t := h.out[id]
		if t != nil && t.Peer == s.peer.ID {
			if t.file != nil {
				_ = t.file.Close()
			}
			t.State = "rejected"
			delete(h.out, id)
		}
		h.mu.Unlock()
		h.toast("warn", "对方拒收了 "+t.Name)
	}
}

// onFileDone 文件块发送完毕，校验。
func (h *Hub) onFileDone(s *Session, m wireMsg) {
	var id [16]byte
	if b := unb64(m.FID); len(b) == 16 {
		copy(id[:], b)
	}
	h.mu.Lock()
	t := h.in[id]
	if t == nil || t.Peer != s.peer.ID {
		h.mu.Unlock()
		return
	}
	// 延迟校验在锁外做。
	t.State = "verifying"
	h.mu.Unlock()
	if t.file == nil {
		return
	}
	path, sha, err := t.file.finish()
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil || sha != t.SHA || t.Recv != t.Size {
		t.file = nil
		t.State = "failed"
		h.removeIncompleteLocked(path)
		delete(h.in, id)
		h.toast("error", "文件 "+t.Name+" 校验失败，已丢弃")
		h.emit("desk:transfer", map[string]interface{}{
			"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": t.Name,
			"dir": "in", "state": "failed", "recv": t.Recv, "size": t.Size,
		})
		return
	}
	t.file = nil
	t.Path = path
	t.State = "done"
	delete(h.in, id)
	h.toast("info", "收到文件 "+t.Name+"，已保存到 "+path)
	h.emit("desk:transfer", map[string]interface{}{
		"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": t.Name,
		"dir": "in", "state": "done", "recv": t.Recv, "size": t.Size, "path": path,
	})
}

// onFileAbort 对端中止了传输（对端取消或对端接收失败）。
func (h *Hub) onFileAbort(s *Session, m wireMsg) {
	var id [16]byte
	if b := unb64(m.FID); len(b) == 16 {
		copy(id[:], b)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if t := h.in[id]; t != nil && t.Peer == s.peer.ID {
		if t.file != nil {
			t.file.abort()
			t.file = nil
		}
		t.State = "cancelled"
		delete(h.in, id)
		h.emit("desk:transfer", map[string]interface{}{
			"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": t.Name,
			"dir": "in", "state": "cancelled", "recv": t.Recv, "size": t.Size,
		})
	}
	if t := h.out[id]; t != nil && t.Peer == s.peer.ID {
		if t.file != nil {
			_ = t.file.Close()
			t.file = nil
		}
		t.cancel.Store(true)
		t.State = "cancelled"
		delete(h.out, id)
		h.cleanupTempUpload(t.Path)
		h.emit("desk:transfer", map[string]interface{}{
			"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": t.Name,
			"dir": "out", "state": "cancelled", "recv": t.Sent, "size": t.Size,
		})
	}
}

// handleChunk 收到文件数据块。
func (h *Hub) handleChunk(s *Session, id [16]byte, seq uint32, data []byte) {
	h.mu.Lock()
	t := h.in[id]
	if t == nil || t.Peer != s.peer.ID || t.file == nil {
		h.mu.Unlock()
		return
	}
	if err := t.file.write(data); err != nil {
		t.file.abort()
		t.file = nil
		t.State = "failed"
		delete(h.in, id)
		h.mu.Unlock()
		_ = s.sendJSON(wireMsg{T: msgFileAbort, FID: b64(id[:])})
		h.toast("error", "文件 "+t.Name+" 接收失败: "+err.Error())
		return
	}
	t.Recv += int64(len(data))
	progress := t.Recv
	size, name := t.Size, t.Name
	// 进度事件限频：至少间隔 200ms，避免大文件刷爆 UI。
	shouldEmit := time.Since(t.lastProgress) >= 200*time.Millisecond
	if shouldEmit {
		t.lastProgress = time.Now()
	}
	h.mu.Unlock()
	if shouldEmit {
		h.emit("desk:transfer", map[string]interface{}{
			"id": hex.EncodeToString(id[:]), "peer": s.PeerName(), "name": name,
			"dir": "in", "state": "receiving", "recv": progress, "size": size,
		})
	}
}

// newTransferID 生成传输 ID。
func newTransferID() [16]byte {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return id
}

// removeIncompleteLocked 删除校验失败的半成品；只允许下载目录内的文件。
func (h *Hub) removeIncompleteLocked(path string) {
	if path == "" || h.cfg.DownloadsDir == "" {
		return
	}
	if !withinDir(h.cfg.DownloadsDir, path) {
		return
	}
	_ = removeFile(path)
}

// CancelTransfer 用户取消一个进行中的传输（收发两侧都适用）。
func (h *Hub) CancelTransfer(fidHex string) {
	raw, err := hex.DecodeString(fidHex)
	if err != nil || len(raw) != 16 {
		return
	}
	var id [16]byte
	copy(id[:], raw)
	// 发送侧：CAS 抢到取消权后同步清理（offer 阶段发送循环尚未启动，必须就地处理）。
	h.mu.Lock()
	if t := h.out[id]; t != nil && (t.State == "offering" || t.State == "sending") {
		if !t.cancel.CompareAndSwap(false, true) {
			h.mu.Unlock()
			return
		}
		peer := t.Peer
		if t.file != nil {
			_ = t.file.Close()
			t.file = nil
		}
		delete(h.out, id)
		h.mu.Unlock()
		if s := func() *Session {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.sessions[peer]
		}(); s != nil && s.Alive() {
			_ = s.sendJSON(wireMsg{T: msgFileAbort, FID: b64(id[:])})
		}
		h.cleanupTempUpload(t.Path)
		h.emit("desk:transfer", map[string]interface{}{
			"id": fidHex, "dir": "out", "state": "cancelled",
		})
		h.toast("info", "已取消发送 "+t.Name)
		return
	}
	if t := h.in[id]; t != nil && (t.State == "offer" || t.State == "receiving") {
		peer := t.Peer
		name := t.Name
		if t.file != nil {
			t.file.abort()
			t.file = nil
		}
		t.State = "cancelled"
		delete(h.in, id)
		h.mu.Unlock()
		if s := func() *Session {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.sessions[peer]
		}(); s != nil {
			_ = s.sendJSON(wireMsg{T: msgFileAbort, FID: b64(id[:])})
		}
		h.emit("desk:transfer", map[string]interface{}{
			"id": fidHex, "dir": "in", "state": "cancelled",
		})
		h.toast("info", "已取消接收 "+name)
		return
	}
	h.mu.Unlock()
}

// markTempUpload 登记浏览器上传产生的临时文件，传输结束后删除。
func (h *Hub) markTempUpload(path string) {
	h.mu.Lock()
	h.tempUploads[path] = true
	h.mu.Unlock()
}

// cleanupTempUpload 传输结束（成功或失败）后清理临时上传文件。
func (h *Hub) cleanupTempUpload(path string) {
	if path == "" {
		return
	}
	h.mu.Lock()
	_, ok := h.tempUploads[path]
	delete(h.tempUploads, path)
	h.mu.Unlock()
	if ok {
		_ = removeFile(path)
	}
}
