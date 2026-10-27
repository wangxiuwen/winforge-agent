package desk

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"sync/atomic"
	"testing"
	"time"
)

// 基本远程桌面端到端：控制发起后被控端开始推屏，控制端收到 desk:screen
// 事件（b64 解码后与假抓屏一致）；释放后事件停止。
func TestScreenStreamOnControl(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	fakeJPEG := makeFakeJPEG(4)
	var sent atomic.Int32
	pb.captureScreen = func(maxW, quality int) ([]byte, int, int, error) {
		sent.Add(1)
		return fakeJPEG, 4, 4, nil
	}
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]

	if err := a.TakeControl(peerB.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 8*time.Second, "A 收到远程屏幕帧", func() bool {
		for _, ev := range emitterOf(a).all() {
			if ev.name != "desk:screen" {
				continue
			}
			if mm, ok := ev.data.(map[string]interface{}); ok {
				if b64, _ := mm["b64"].(string); b64 != "" {
					if dec, err := base64.StdEncoding.DecodeString(b64); err == nil &&
						bytes.Equal(dec, fakeJPEG) && mm["w"] == 4 && mm["h"] == 4 {
						return true
					}
				}
			}
		}
		return false
	})

	// 释放后推流应停止：记录事件数，1.5s 后不增长。
	a.ReleaseControl(0.5)
	waitFor(t, 5*time.Second, "A 回到空闲", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.ctl.Role == roleIdle
	})
	time.Sleep(1500 * time.Millisecond)
	count := 0
	for _, ev := range emitterOf(a).all() {
		if ev.name == "desk:screen" {
			count++
		}
	}
	time.Sleep(1500 * time.Millisecond)
	count2 := 0
	for _, ev := range emitterOf(a).all() {
		if ev.name == "desk:screen" {
			count2++
		}
	}
	if count2 != count {
		t.Fatalf("释放后 screen 事件应停止: %d → %d", count, count2)
	}
}

func makeFakeJPEG(side int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for x := 0; x < side; x++ {
		for y := 0; y < side; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// 静止画面跳帧：相同帧不重复发送；画面变化后恢复发送。
func TestScreenStreamSkipsStaticFrames(t *testing.T) {
	pa, pb := newFakePlatform(), newFakePlatform()
	alt := int32(0)
	pb.captureScreen = func(maxW, quality int) ([]byte, int, int, error) {
		n := atomic.AddInt32(&alt, 1)
		// 每 3 次抓屏换一次颜色 → 前 3 帧同色只发 1 帧，之后变化再发
		c := uint8(100 + (n/3)%2*100)
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for x := 0; x < 4; x++ {
			for y := 0; y < 4; y++ {
				img.Set(x, y, color.RGBA{R: c, G: 30, B: 40, A: 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
			return nil, 0, 0, err
		}
		return buf.Bytes(), 4, 4, nil
	}
	a, _ := newTestHub(t, "主机A", pa)
	b, _ := newTestHub(t, "主机B", pb)
	pairTwo(t, a, b)
	peerB := peerViews(a)[0]
	if err := a.TakeControl(peerB.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	// 被控端抓 12 次（4fps×3s），静态组 4 帧 + 变化组各 4 帧 → 唯一帧 ≤3 种
	waitFor(t, 5*time.Second, "收到首帧", func() bool {
		for _, ev := range emitterOf(a).all() {
			if ev.name == "desk:screen" {
				return true
			}
		}
		return false
	})
	time.Sleep(2500 * time.Millisecond)
	a.ReleaseControl(0.5)
	// 统计唯一帧内容数：3 色轮换最多 3 种，重复同色帧必须被跳过
	uniq := map[string]bool{}
	for _, ev := range emitterOf(a).all() {
		if ev.name == "desk:screen" {
			if mm, ok := ev.data.(map[string]interface{}); ok {
				if b64, _ := mm["b64"].(string); b64 != "" {
					uniq[b64] = true
				}
			}
		}
	}
	t.Logf("唯一帧数: %d", len(uniq))
	if len(uniq) > 3 {
		t.Fatalf("静态帧未被跳过: 唯一帧 %d > 3", len(uniq))
	}
	if len(uniq) < 2 {
		t.Fatalf("画面变化应产生新帧: 唯一帧 %d", len(uniq))
	}
}
