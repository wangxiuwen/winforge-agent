package desk

import (
	"time"
)

// 输入层把平台相关（钩子/注入）与平台无关（虚拟坐标、按键集合、边缘判定）
// 分开：本文件是纯逻辑，input_windows.go 提供真实实现，input_other.go 提供占位。

// InputEvent 是一次捕获到的键鼠事件。
type InputEvent struct {
	Kind   InputKind
	VK     uint32 // 键盘：虚拟键码
	Scan   uint32 // 键盘：扫描码
	Ext    bool   // 键盘：扩展键
	Down   bool
	MoveX  int32 // 鼠标移动：相对屏幕中心的原始位移（捕获方积分前）
	MoveY  int32
	Button MouseButton // 鼠标按键
	Wheel  int32       // 滚轮刻度，正为前/右
}

// InputKind 区分事件类型。
type InputKind uint8

const (
	InputMouseMove InputKind = iota
	InputButton
	InputWheel
	InputKey
)

// MouseButton 编码鼠标按键。
type MouseButton uint8

const (
	ButtonLeft MouseButton = iota + 1
	ButtonRight
	ButtonMiddle
	ButtonX1
	ButtonX2
)

// ScreenBounds 是一台机器的虚拟屏幕范围（虚拟桌面坐标）。
type ScreenBounds struct {
	X, Y, W, H int32
}

// VirtualPos 是捕获方的“想象光标”位置，可以超出物理屏幕指向相邻机器。
type VirtualPos struct {
	X, Y float64
}

// Integrator 把原始鼠标位移积分成虚拟坐标，并判断“顶边”意图。
type Integrator struct {
	Bounds ScreenBounds
	Pos    VirtualPos
	// clampStreak 连续贴边且有继续同方向位移的事件数。
	clampStreak  int
	lastClampDir int32
	lastClampAt  time.Time
}

// NewIntegrator 以当前物理光标位置初始化。
func NewIntegrator(b ScreenBounds, startX, startY float64) *Integrator {
	return &Integrator{Bounds: b, Pos: VirtualPos{X: startX, Y: startY}}
}

// Move 积分一次位移，返回本次后的虚拟坐标。
func (in *Integrator) Move(dx, dy int32) VirtualPos {
	in.Pos.X += float64(dx)
	in.Pos.Y += float64(dy)
	if in.Pos.X < float64(in.Bounds.X) {
		in.Pos.X = float64(in.Bounds.X)
	}
	if in.Pos.Y < float64(in.Bounds.Y) {
		in.Pos.Y = float64(in.Bounds.Y)
	}
	if in.Pos.X > float64(in.Bounds.X+in.Bounds.W) {
		in.Pos.X = float64(in.Bounds.X + in.Bounds.W)
	}
	if in.Pos.Y > float64(in.Bounds.Y+in.Bounds.H) {
		in.Pos.Y = float64(in.Bounds.Y + in.Bounds.H)
	}
	return in.Pos
}

// EdgeIntent 判断是否表达“越过某边”的意图：
// 虚拟坐标贴边且继续朝该方向移动，300ms 内累计 3 次即触发。
type Edge int

const (
	EdgeNone Edge = iota
	EdgeLeft
	EdgeRight
	EdgeTop
	EdgeBottom
)

const (
	edgeStreakNeeded = 3
	edgeStreakWindow = 300 * time.Millisecond
	edgeClampEpsilon = 1.5
)

func (in *Integrator) EdgeIntent(now time.Time) Edge {
	b := in.Bounds
	atLeft := in.Pos.X <= float64(b.X)+edgeClampEpsilon
	atRight := in.Pos.X >= float64(b.X+b.W)-edgeClampEpsilon
	atTop := in.Pos.Y <= float64(b.Y)+edgeClampEpsilon
	atBottom := in.Pos.Y >= float64(b.Y+b.H)-edgeClampEpsilon
	if !atLeft && !atRight && !atTop && !atBottom {
		in.clampStreak = 0
		return EdgeNone
	}
	if now.Sub(in.lastClampAt) > edgeStreakWindow {
		in.clampStreak = 0
	}
	in.clampStreak++
	in.lastClampAt = now
	if in.clampStreak < edgeStreakNeeded {
		return EdgeNone
	}
	switch {
	case atLeft:
		return EdgeLeft
	case atRight:
		return EdgeRight
	case atTop:
		return EdgeTop
	case atBottom:
		return EdgeBottom
	}
	return EdgeNone
}

// Normalize 把虚拟坐标换算为对端 SendInput 的 0..65535 归一化值。
func Normalize(p VirtualPos, b ScreenBounds) (uint32, uint32) {
	nx, ny := 0.0, 0.0
	if b.W > 1 {
		nx = (p.X - float64(b.X)) / float64(b.W-1)
	}
	if b.H > 1 {
		ny = (p.Y - float64(b.Y)) / float64(b.H-1)
	}
	if nx < 0 {
		nx = 0
	}
	if ny < 0 {
		ny = 0
	}
	if nx > 1 {
		nx = 1
	}
	if ny > 1 {
		ny = 1
	}
	return uint32(nx*65535 + 0.5), uint32(ny*65535 + 0.5)
}

// EdgeEntryYRatio 计算从某边进入对端时的初始光标 Y 比例。
func EdgeEntryYRatio(yRatio float64) float64 {
	if yRatio < 0 {
		return 0.5
	}
	if yRatio > 1 {
		return 1
	}
	return yRatio
}

// PressedKeys 跟踪已按下（已转发/已注入）的键，用于切换时统一下发 keyup。
type PressedKeys struct {
	keys map[uint32]struct{}
}

func NewPressedKeys() *PressedKeys { return &PressedKeys{keys: map[uint32]struct{}{}} }

// Observe 记录一次按键事件。
func (p *PressedKeys) Observe(vk uint32, down bool) {
	if down {
		p.keys[vk] = struct{}{}
	} else {
		delete(p.keys, vk)
	}
}

// List 返回当前按下的键。
func (p *PressedKeys) List() []uint32 {
	out := make([]uint32, 0, len(p.keys))
	for k := range p.keys {
		out = append(out, k)
	}
	return out
}

// Clear 清空记录（本地状态怀疑失步时）。
func (p *PressedKeys) Clear() { p.keys = map[uint32]struct{}{} }

// Capturer 是键鼠捕获接口（Windows: 低级钩子实现）。
type Capturer interface {
	// Start 开始捕获。事件发到 events；suppress 表示本地吞掉输入。
	Start(events chan<- InputEvent) error
	Stop()
	Active() bool
}

// Injector 是键鼠注入接口（Windows: SendInput 实现）。
type Injector interface {
	InjectMove(normX, normY uint32)
	InjectButton(b MouseButton, down bool)
	InjectWheel(delta int32)
	InjectKey(vk, scan uint32, ext, down bool)
	// ScreenBounds 返回本机虚拟屏幕。
	ScreenBounds() ScreenBounds
	// SetCursor 把物理光标移到虚拟屏幕坐标。
	SetCursor(x, y int32)
	GetCursor() (int32, int32)
	// ReleaseAll 释放记录中的所有按键与鼠标键。
	ReleaseAll()
}
