//go:build windows

package desk

import (
	"image"
	"testing"
)

func TestDrawCursorMarkerIsolated(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 225))
	drawCursorMarker(img, 0, 0, 1920, 1080, 400, 225)
	red := 0
	for y := 0; y < 225; y++ {
		for x := 0; x < 400; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 40000 && g < 20000 && b < 20000 {
				red++
			}
		}
	}
	t.Logf("red pixels: %d", red)
	if red == 0 {
		t.Fatal("drawCursorMarker 未画出任何红像素")
	}
}
