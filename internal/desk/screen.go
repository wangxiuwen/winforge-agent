package desk

import (
	"encoding/base64"
	"encoding/binary"
	"image/jpeg"
	"image"
	"bytes"
	"time"
)

// 基本远程桌面：被控端在被控期间按帧抓屏（JPEG）推给控制端，
// 控制端经 desk:screen 事件在控制视图显示。v1 全帧无差异编码。

const (
	screenDefaultFPS    = 3
	screenMaxWidth      = 1600
	screenJPEGQuality   = 60
	screenFrameHdrLen   = 12 // [4B W][4B H][4B BE seq]
)

// screenLoopState 每 Hub 一份的推流循环状态。
type screenLoopState struct {
	stop chan struct{}
	seq  uint32
}

// startScreenLoop 被控端开始向 s 推屏（已在推则忽略）。
func (h *Hub) startScreenLoop(s *Session, fps int) {
	if fps <= 0 || fps > 15 {
		fps = screenDefaultFPS
	}
	capture := h.platform.CaptureScreen
	if capture == nil {
		return
	}
	h.mu.Lock()
	if h.screenLoop != nil {
		h.mu.Unlock()
		return
	}
	st := &screenLoopState{stop: make(chan struct{})}
	h.screenLoop = st
	h.mu.Unlock()
	safeGo(h.log, "screen-loop", func() {
		interval := time.Second / time.Duration(fps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-st.stop:
				return
			case <-ticker.C:
			}
			// 角色不再是被控（对方已拿回/本端退出）即自停。
			h.mu.Lock()
			role := h.ctl.Role
			h.mu.Unlock()
			if role != roleControlled {
				h.stopScreenLoop()
				return
			}
			jpegBytes, w, ht, err := capture(screenMaxWidth, screenJPEGQuality)
			if err != nil {
				continue
			}
			st.seq++
			payload := make([]byte, screenFrameHdrLen+len(jpegBytes))
			binary.BigEndian.PutUint32(payload[0:4], uint32(w))
			binary.BigEndian.PutUint32(payload[4:8], uint32(ht))
			binary.BigEndian.PutUint32(payload[8:12], st.seq)
			copy(payload[screenFrameHdrLen:], jpegBytes)
			if err := s.sendScreenFrame(payload); err != nil {
				h.stopScreenLoop()
				return
			}
		}
	})
}

// stopScreenLoop 停止推流循环（幂等）。
func (h *Hub) stopScreenLoop() {
	h.mu.Lock()
	st := h.screenLoop
	h.screenLoop = nil
	h.mu.Unlock()
	if st != nil {
		close(st.stop)
	}
}

// onScreenFrame 控制端收到一帧：重组头部并 emit desk:screen 给 UI。
func (h *Hub) onScreenFrame(payload []byte) {
	if len(payload) < screenFrameHdrLen {
		return
	}
	w := int(binary.BigEndian.Uint32(payload[0:4]))
	ht := int(binary.BigEndian.Uint32(payload[4:8]))
	jpegBytes := payload[screenFrameHdrLen:]
	if w <= 0 || ht <= 0 || len(jpegBytes) == 0 {
		return
	}
	h.emit("desk:screen", map[string]interface{}{
		"w":   w,
		"h":   ht,
		"b64": base64.StdEncoding.EncodeToString(jpegBytes),
	})
}

// encodeScreenJPEG 供测试与未来复用：内存图像转 JPEG。
func encodeScreenJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
