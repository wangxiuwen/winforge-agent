//go:build windows

package desk

import (
	"bytes"
	"image/jpeg"
	"testing"
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
