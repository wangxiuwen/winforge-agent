//go:build windows

package desk

import (
	"bytes"
	"fmt"
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
	// 测试进程声明 DPI 感知：否则 GetSystemMetrics/SetCursorPos/GetCursorPos
	// 全被系统虚拟化，坐标换算不一致（R69 实测光标被画到左边缘）。
	user32.NewProc("SetProcessDPIAware").Call()
	var pt pointScr
	// 虚拟屏中心（多屏机器与抓屏范围一致）
	vx, _, _ := user32.NewProc("GetSystemMetrics").Call(76)
	vy, _, _ := user32.NewProc("GetSystemMetrics").Call(77)
	cw, _, _ := user32.NewProc("GetSystemMetrics").Call(78)
	chh, _, _ := user32.NewProc("GetSystemMetrics").Call(79)
	pt.X = int32(vx) + int32(cw)/2
	pt.Y = int32(vy) + int32(chh)/2
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
	// 标记应画在光标（虚拟屏中心）在缩放帧里的映射位置
	cx := img.Bounds().Min.X + img.Bounds().Dx()/2
	cy := img.Bounds().Min.Y + img.Bounds().Dy()/2
	found := false
	// 只在光标预期映射位置附近计数（真实桌面可能有别的红色元素）
	red := 0
	for y := cy - 15; y <= cy+15; y++ {
		for x := cx - 15; x <= cx+15; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 50000 && g < 20000 && b < 20000 {
				red++
			}
		}
	}
	if red >= 15 { // r=5 红心在 400 宽帧上约 80px，桌面内容干扰后 15 仍稳
		found = true
	}
	if !found {
		redAt := ""
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 50000 && g < 20000 && b < 20000 && len(redAt) < 40 {
					redAt += fmt.Sprintf("(%d,%d)", x, y)
				}
			}
		}
		t.Fatalf("虚拟屏中心 %d,%d 附近未找到光标红点（帧 %dx%d, red=%d, redAt=%s）",
			cx, cy, w, h, red, redAt)
	}
}
