package desk

import "testing"


// 积分器：位移积分 + 边界钳制（跨分辨率注入的坐标基准）。
func TestIntegratorClamp(t *testing.T) {
	in := NewIntegrator(ScreenBounds{X: 0, Y: 0, W: 1920, H: 1080}, 960, 540)
	p := in.Move(5000, -8000) // 大位移一次打到边界外
	if p.X != 1920 || p.Y != 0 {
		t.Fatalf("应钳制到边界内: %+v", p)
	}
	p = in.Move(-5000, 5000)
	if p.X != 0 || p.Y != 1080 {
		t.Fatalf("反向应钳制到另一边界: %+v", p)
	}
}
