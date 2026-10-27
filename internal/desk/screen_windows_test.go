//go:build windows

package desk

import (
	"bytes"
	"image/jpeg"
	"testing"
	"time"
)

// 真机 GDI 抓屏验证：应产出可解码的真实 JPEG（本机跑，CI Linux 不编译本文件）。
func TestCaptureScreenGDI(t *testing.T) {
	jpegBytes, w, h, err := captureScreenGDI(1600, 60)
	if err != nil {
		t.Fatal(err)
	}
	if w <= 0 || h <= 0 || w > 1600 {
		t.Fatalf("尺寸异常: %dx%d", w, h)
	}
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("产物不是合法 JPEG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		t.Fatalf("JPEG 尺寸与声明不符: %dx%d vs %dx%d", b.Dx(), b.Dy(), w, h)
	}
	t.Logf("抓屏 OK: %dx%d, %d bytes", w, h, len(jpegBytes))
}

// 光标标记断言：把光标设到屏幕中心，抓帧后中心像素应为红心白环。
func TestCaptureCursorMarkerAtCenter(t *testing.T) {
	var pt pointScr
	sw, _, _ := user32.NewProc("GetSystemMetrics").Call(0)
	sh, _, _ := user32.NewProc("GetSystemMetrics").Call(1)
	pt.X = int32(int32(sw) / 2)
	pt.Y = int32(int32(sh) / 2)
	user32.NewProc("SetCursorPos").Call(uintptr(pt.X), uintptr(pt.Y))
	time.Sleep(150 * time.Millisecond)

	jpegBytes, w, h, err := captureScreenGDI(400, 60)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatal(err)
	}
	cx := img.Bounds().Min.X + img.Bounds().Dx()/2
	cy := img.Bounds().Min.Y + img.Bounds().Dy()/2
	found := false
	for dy := -12; dy <= 12 && !found; dy++ {
		for dx := -12; dx <= 12 && !found; dx++ {
			r, g, b, _ := img.At(cx+dx, cy+dy).RGBA()
			// 红心：R 高、G/B 低
			if r > 40000 && g < 20000 && b < 20000 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("屏幕中心 %d,%d 附近未找到光标红点（帧 %dx%d）", cx, cy, w, h)
	}
}
