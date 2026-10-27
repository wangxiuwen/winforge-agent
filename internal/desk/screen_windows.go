//go:build windows

package desk

import (
	"bytes"
	"image"
	"image/jpeg"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 远程桌面抓屏：GDI BitBlt 主屏 → 32bpp DIB → RGBA → JPEG。
// v1 全帧无差异编码，宽超过 maxW 时按比例缩小（GO 内最近邻采样）。

var (
	screenGdi32         = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC           = user32.NewProc("GetDC")
	procReleaseDC       = user32.NewProc("ReleaseDC")
	procGetCursorPosScr = user32.NewProc("GetCursorPos")
	procCreateCompatibleDC = screenGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = screenGdi32.NewProc("DeleteDC")
	procCreateDIBSection   = screenGdi32.NewProc("CreateDIBSection")
	procSelectObjectScr    = screenGdi32.NewProc("SelectObject")
	procDeleteObjectScr    = screenGdi32.NewProc("DeleteObject")
	procBitBltScr          = screenGdi32.NewProc("BitBlt")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srccopyScr = 0x00CC0020
	dibRGBScr  = 0 // BI_RGB
	dibColors  = 0 // DIB_RGB_COLORS
)

type bitmapInfoHeaderScr struct {
	Size          uint32
	Width         int32
	Height        int32 // 负值 = 自顶向下
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfoScr struct {
	Header bitmapInfoHeaderScr
	Colors [1]uint32
}

// captureScreenGDI 实现 Platform.CaptureScreen。
func captureScreenGDI(maxW, quality int) ([]byte, int, int, error) {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, 0, 0, errScreenCapture("GetDC 失败")
	}
	defer procReleaseDC.Call(0, hdc)

	sw, _, _ := user32.NewProc("GetSystemMetrics").Call(smCXScreen)
	sh, _, _ := user32.NewProc("GetSystemMetrics").Call(smCYScreen)
	swI, shI := int32(sw), int32(sh)
	if swI <= 0 || shI <= 0 {
		return nil, 0, 0, errScreenCapture("屏幕尺寸异常")
	}
	outW, outH := swI, shI
	if int(outW) > maxW {
		outW = int32(maxW)
		outH = shI * outW / swI
	}

	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, 0, 0, errScreenCapture("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(memDC)

	bmi := bitmapInfoScr{}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.Width = outW
	bmi.Header.Height = -outH
	bmi.Header.Planes = 1
	bmi.Header.BitCount = 32
	bmi.Header.Compression = dibRGBScr

	var bits uintptr
	hbm, _, err2 := procCreateDIBSection.Call(hdc,
		uintptr(unsafe.Pointer(&bmi)), dibColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == 0 {
		return nil, 0, 0, errScreenCapture("CreateDIBSection 失败: " + err2.Error())
	}
	defer procDeleteObjectScr.Call(hbm)

	procSelectObjectScr.Call(memDC, hbm)
	ok, _, err3 := procBitBltScr.Call(memDC,
		0, 0, uintptr(outW), uintptr(outH),
		hdc, 0, 0, uintptr(srccopyScr))
	if ok == 0 {
		return nil, 0, 0, errScreenCapture("BitBlt 失败: " + err3.Error())
	}

	// BGRA → RGBA（JPEG 忽略 alpha）
	pix := unsafe.Slice((*byte)(unsafe.Pointer(bits)), int(outW)*int(outH)*4)
	img := image.NewRGBA(image.Rect(0, 0, int(outW), int(outH)))
	for i, j := 0, 0; j < len(img.Pix); i, j = i+4, j+4 {
		img.Pix[j+0] = pix[i+2] // R
		img.Pix[j+1] = pix[i+1] // G
		img.Pix[j+2] = pix[i+0] // B
		img.Pix[j+3] = 0xFF
	}
	drawCursorMarker(img, swI, shI, int(outW), int(outH))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), int(outW), int(outH), nil
}

type screenCaptureError string

func (e screenCaptureError) Error() string { return string(e) }

func errScreenCapture(msg string) error { return screenCaptureError(msg) }

type pointScr struct{ X, Y int32 }

// drawCursorMarker 把被控端物理光标（即控制者移动的光标）画成白边红点，
// 让控制端有位置反馈。sw/sh 为抓屏时的屏幕原尺寸（光标是屏幕坐标）。
func drawCursorMarker(img *image.RGBA, sw, sh int32, outW, outH int) {
	var pt pointScr
	// 注意：LazyProc.Call 的 err 是 GetLastError，成功时也常非 nil——必须判 r1。
	if r1, _, _ := procGetCursorPosScr.Call(uintptr(unsafe.Pointer(&pt))); r1 == 0 {
		return
	}
	if sw <= 0 || sh <= 0 {
		return
	}
	cx := int(float64(pt.X) / float64(sw) * float64(outW))
	cy := int(float64(pt.Y) / float64(sh) * float64(outH))
	const rOuter, rInner = 9, 5 // 白边红心
	b := img.Rect
	for dy := -rOuter; dy <= rOuter; dy++ {
		for dx := -rOuter; dx <= rOuter; dx++ {
			d2 := dx*dx + dy*dy
			if d2 > rOuter*rOuter {
				continue
			}
			x, y := cx+dx, cy+dy
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			off := img.PixOffset(x, y)
			if d2 <= rInner*rInner {
				img.Pix[off+0], img.Pix[off+1], img.Pix[off+2] = 0xE6, 0x2F, 0x2F // 红
			} else {
				img.Pix[off+0], img.Pix[off+1], img.Pix[off+2] = 0xFF, 0xFF, 0xFF // 白边
			}
		}
	}
}
