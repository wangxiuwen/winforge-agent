//go:build windows

package desk

import (
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 键鼠平台层：
//   捕获：WH_KEYBOARD_LL / WH_MOUSE_LL 低级钩子，捕获期间返回 1 吞掉本地输入；
//        鼠标移动用“中心回卷”积分原始位移——LL 回调拿到的 pt 是系统基于真实
//        光标算的，输入被吞后 pt 不再前进，必须自己积分。
//   注入：SendInput；键盘走扫描码（KEYEVENTF_SCANCODE）以穿透多数应用，
//        鼠标用虚拟桌面绝对坐标。注入事件带 INJECTED 标记，钩子直接放行。
//   边缘：非捕获期间观察光标贴边事件，300ms 内连续 3 次判定为“推挤”意图。
//   热键：RegisterHotKey，消息循环里收 WM_HOTKEY。
// 钩子回调必须快进快出（阻塞超 300ms 系统会摘钩），只做入队，不做业务。

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx  = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procGetMessage        = user32.NewProc("GetMessageW")
	procPostThreadMessage = user32.NewProc("PostThreadMessageW")
	procSendInput         = user32.NewProc("SendInput")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procSetCursorPos      = user32.NewProc("SetCursorPos")
	procRegisterHotKey    = user32.NewProc("RegisterHotKey")
)

const (
	wH_KEYBOARD_LL = 13
	wH_MOUSE_LL    = 14

	wM_QUIT   = 0x0012
	wM_HOTKEY = 0x0312

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	llkhfInjected = 0x10
	llkhfExtended = 0x20
	llmhfInjected = 0x01

	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmMouseWheel  = 0x020A
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	wmMouseHWheel = 0x020E

	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	keyeventfScanCode    = 0x0008

	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfMiddleDown  = 0x0020
	mouseeventfMiddleUp    = 0x0040
	mouseeventfWheel       = 0x0800
	mouseeventfHWheel      = 0x1000
	mouseeventfVirtualDesk = 0x4000
	mouseeventfAbsolute    = 0x8000

	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
)

type winPoint struct{ X, Y int32 }

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
}

type kbdllhookstruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type msllhookstruct struct {
	Pt        winPoint
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keyboardInput struct {
	Type      uint32
	WVk       uint16
	WScan     uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type mouseInput struct {
	Type      uint32
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winInput struct {
	Type     uint32
	Mouse    mouseInput
	Keyboard keyboardInput
}

// windowsInput 实现 Capturer/Injector/边缘观察/热键。
type windowsInput struct {
	mode atomic.Int32                    // 0 空闲 1 捕获中
	sink atomic.Pointer[chan InputEvent] // 捕获事件去向
	edge atomic.Pointer[chan edgeHit]    // 边缘观察去向

	kbHook    atomic.Uintptr
	mouseHook atomic.Uintptr
	threadID  atomic.Uint32

	centerPoint atomic.Uint64 // 屏幕中心（捕获时回卷点）

	// 注入端按键跟踪（SendInput 不会自己记）。
	injMu      sync.Mutex
	injKeys    map[uint32]uint32 // vk -> scan
	injButtons map[MouseButton]bool

	hotkeyCh chan struct{}
	escapeCh chan struct{}
}

type edgeHit struct {
	edge   Edge
	yRatio float64
}

// NewPlatform 构造 Windows 平台实现并启动钩子线程。
func NewPlatform(hotkey string, logger *log.Logger) *Platform {
	wi := &windowsInput{
		hotkeyCh:   make(chan struct{}, 1),
		escapeCh:   make(chan struct{}, 1),
		injKeys:    map[uint32]uint32{},
		injButtons: map[MouseButton]bool{},
	}
	wi.startThread(hotkey, logger)
	setIndicator, err := startEdgeIndicator()
	if err != nil {
		logger.Printf("边缘提示条不可用: %v", err)
	}
	return &Platform{
		Injector: func() (Injector, error) { return wi, nil },
		StartCapture: func(ch chan InputEvent) (func(), error) {
			wi.refreshCenter()
			wi.sink.Store((*chan InputEvent)(&ch))
			wi.mode.Store(1)
			return func() {
				wi.mode.Store(0)
				wi.sink.Store(nil)
			}, nil
		},
		WatchEdges: wi.watchEdges,
		Hotkey:     wi.hotkeyCh,
		Escape:     wi.escapeCh,
		ScreenBounds: func() ScreenBounds {
			return wi.screenBounds()
		},
		CursorPos: func() (int32, int32) {
			var p winPoint
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
			return p.X, p.Y
		},
		SetCursor: func(x, y int32) {
			procSetCursorPos.Call(uintptr(int32(x)), uintptr(int32(y)))
		},
		SetControlIndicator: setIndicator,
	}
}

// startThread 启动常驻钩子线程：装钩子、注册热键、跑消息循环。
func (w *windowsInput) startThread(hotkey string, logger *log.Logger) {
	go func() {
		runtime.LockOSThread()
		kbCB := syscall.NewCallback(w.keyboardProc)
		mouseCB := syscall.NewCallback(w.mouseProc)
		if hk, _, _ := procSetWindowsHookEx.Call(wH_KEYBOARD_LL, kbCB, 0, 0); hk != 0 {
			w.kbHook.Store(hk)
		} else {
			logger.Printf("键盘钩子安装失败")
		}
		if hk, _, _ := procSetWindowsHookEx.Call(wH_MOUSE_LL, mouseCB, 0, 0); hk != 0 {
			w.mouseHook.Store(hk)
		} else {
			logger.Printf("鼠标钩子安装失败")
		}
		w.refreshCenter()
		mods, vk, ok := parseHotkey(hotkey)
		if ok {
			if r, _, _ := procRegisterHotKey.Call(0, 1, uintptr(mods), uintptr(vk)); r == 0 {
				logger.Printf("热键 %s 注册失败（可能被占用），改用界面按钮切换", hotkey)
			}
		}
		// 消息循环同时驱动低级钩子与 WM_HOTKEY。
		var m winMsg
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			if m.Message == wM_HOTKEY {
				select {
				case w.hotkeyCh <- struct{}{}:
				default:
				}
			}
		}
	}()
}

// setCenter 更新捕获回卷中心。
func (w *windowsInput) refreshCenter() {
	b := w.screenBounds()
	var v uint64
	v |= uint64(uint32(b.X+b.W/2)) | uint64(uint32(b.Y+b.H/2))<<32
	w.centerPoint.Store(v)
}

func (w *windowsInput) center() winPoint {
	v := w.centerPoint.Load()
	return winPoint{X: int32(uint32(v)), Y: int32(uint32(v >> 32))}
}

// keyboardProc WH_KEYBOARD_LL 回调。
func (w *windowsInput) keyboardProc(nCode int, wparam, lparam uintptr) uintptr {
	if nCode >= 0 {
		kb := (*kbdllhookstruct)(unsafe.Pointer(lparam))
		if kb.Flags&llkhfInjected == 0 {
			// 非捕获模式：被控端物理 Esc 快捷交还。
			if w.mode.Load() != 1 && kb.VkCode == 0x1B &&
				(wparam == uintptr(wmKeyDown) || wparam == uintptr(wmSysKeyDown)) {
				select {
				case w.escapeCh <- struct{}{}:
				default:
				}
			}
			if w.mode.Load() == 1 {
				down := wparam == uintptr(wmKeyDown) || wparam == uintptr(wmSysKeyDown)
				ev := InputEvent{Kind: InputKey, VK: kb.VkCode, Scan: kb.ScanCode,
					Ext: kb.Flags&llkhfExtended != 0, Down: down}
				w.pushCapture(ev)
				return 1 // 吞掉
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wparam, lparam)
	return r
}

// mouseProc WH_MOUSE_LL 回调。
func (w *windowsInput) mouseProc(nCode int, wparam, lparam uintptr) uintptr {
	if nCode >= 0 {
		ms := (*msllhookstruct)(unsafe.Pointer(lparam))
		if ms.Flags&llmhfInjected == 0 {
			if w.mode.Load() == 1 {
				switch wparam {
				case uintptr(wmMouseMove):
					c := w.center()
					dx, dy := ms.Pt.X-c.X, ms.Pt.Y-c.Y
					w.pushCapture(InputEvent{Kind: InputMouseMove, MoveX: dx, MoveY: dy})
					procSetCursorPos.Call(uintptr(c.X), uintptr(c.Y)) // 回卷，保持位移可积分
				case uintptr(wmLButtonDown):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonLeft, Down: true})
				case uintptr(wmLButtonUp):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonLeft, Down: false})
				case uintptr(wmRButtonDown):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonRight, Down: true})
				case uintptr(wmRButtonUp):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonRight, Down: false})
				case uintptr(wmMButtonDown):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonMiddle, Down: true})
				case uintptr(wmMButtonUp):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonMiddle, Down: false})
				case uintptr(wmXButtonDown):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonX1, Down: true})
				case uintptr(wmXButtonUp):
					w.pushCapture(InputEvent{Kind: InputButton, Button: ButtonX1, Down: false})
				case uintptr(wmMouseWheel):
					w.pushCapture(InputEvent{Kind: InputWheel, Wheel: int32(int16(ms.MouseData >> 16))})
				case uintptr(wmMouseHWheel):
					w.pushCapture(InputEvent{Kind: InputWheel, Wheel: int32(int16(ms.MouseData >> 16))})
				}
				return 1 // 吞掉
			}
			// 空闲/被控模式：喂边缘观察。
			if ec := w.edge.Load(); ec != nil && wparam == uintptr(wmMouseMove) {
				if hit, ok := detectEdgeHit(w.screenBounds(), ms.Pt); ok {
					select {
					case *ec <- hit:
					default:
					}
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wparam, lparam)
	return r
}

// detectEdgeHit 光标贴边判定。
func detectEdgeHit(b ScreenBounds, p winPoint) (edgeHit, bool) {
	const eps = 2
	switch {
	case p.X <= b.X+int32(eps):
		return edgeHit{EdgeLeft, yRatioOf(b, p.Y)}, true
	case p.X >= b.X+b.W-int32(eps):
		return edgeHit{EdgeRight, yRatioOf(b, p.Y)}, true
	case p.Y <= b.Y+int32(eps):
		return edgeHit{EdgeTop, 0}, true
	case p.Y >= b.Y+b.H-int32(eps):
		return edgeHit{EdgeBottom, 0}, true
	}
	return edgeHit{}, false
}

func yRatioOf(b ScreenBounds, y int32) float64 {
	if b.H <= 1 {
		return 0.5
	}
	r := float64(y-b.Y) / float64(b.H)
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// pushCapture 入队捕获事件，满了丢弃（宁丢不卡钩子）。
func (w *windowsInput) pushCapture(ev InputEvent) {
	if ch := w.sink.Load(); ch != nil {
		select {
		case *ch <- ev:
		default:
		}
	}
}

// watchEdges 订阅边缘意图（去抖后回调）。
func (w *windowsInput) watchEdges(cb func(Edge, float64)) (func(), error) {
	ch := make(chan edgeHit, 128)
	w.edge.Store(&ch)
	go func() {
		var streak Edge
		var count int
		var last time.Time
		var lastY float64
		for hit := range ch {
			now := time.Now()
			if now.Sub(last) > 300*time.Millisecond || hit.edge != streak {
				streak, count = hit.edge, 0
			}
			count++
			last = now
			lastY = hit.yRatio
			if count >= 3 {
				count = 0
				cb(hit.edge, lastY)
			}
		}
	}()
	return func() {
		w.edge.Store(nil)
		// 不关 ch：钩子线程可能还在投递，让它被 GC。
	}, nil
}

// screenBounds 虚拟桌面范围（小写为内部使用）。
func (w *windowsInput) screenBounds() ScreenBounds {
	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	cx, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	return ScreenBounds{X: int32(x), Y: int32(y), W: int32(cx), H: int32(cy)}
}

// ScreenBounds 实现 Injector 接口。
func (w *windowsInput) ScreenBounds() ScreenBounds { return w.screenBounds() }

// SetCursor 实现 Injector 接口。
func (w *windowsInput) SetCursor(x, y int32) {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
}

// ---- Injector 实现 ----

// GetCursor 当前物理光标（虚拟桌面坐标）。
func (w *windowsInput) GetCursor() (int32, int32) {
	var p winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y
}

func (w *windowsInput) InjectMove(nx, ny uint32) {
	b := w.screenBounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	x := b.X + int32((uint64(nx)*uint64(b.W))>>16)
	y := b.Y + int32((uint64(ny)*uint64(b.H))>>16)
	inp := winInput{Type: inputMouse}
	inp.Mouse = mouseInput{Type: inputMouse, DX: x, DY: y,
		Flags: mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualDesk}
	sendInput(&inp)
}

func (w *windowsInput) InjectButton(b MouseButton, down bool) {
	inp := winInput{Type: inputMouse}
	switch b {
	case ButtonLeft:
		if down {
			inp.Mouse.Flags = mouseeventfLeftDown
		} else {
			inp.Mouse.Flags = mouseeventfLeftUp
		}
	case ButtonRight:
		if down {
			inp.Mouse.Flags = mouseeventfRightDown
		} else {
			inp.Mouse.Flags = mouseeventfRightUp
		}
	case ButtonMiddle:
		if down {
			inp.Mouse.Flags = mouseeventfMiddleDown
		} else {
			inp.Mouse.Flags = mouseeventfMiddleUp
		}
	default:
		return
	}
	w.injMu.Lock()
	w.injButtons[b] = down
	w.injMu.Unlock()
	sendInput(&inp)
}

func (w *windowsInput) InjectWheel(delta int32) {
	inp := winInput{Type: inputMouse}
	inp.Mouse = mouseInput{Type: inputMouse, MouseData: uint32(int16(delta)), Flags: mouseeventfWheel}
	sendInput(&inp)
}

func (w *windowsInput) InjectKey(vk, scan uint32, ext, down bool) {
	inp := winInput{Type: inputKeyboard}
	flags := uint32(keyeventfScanCode)
	if ext {
		flags |= keyeventfExtendedKey
	}
	if !down {
		flags |= keyeventfKeyUp
	}
	if scan == 0 {
		scan = mapVirtualKey(vk)
	}
	inp.Keyboard = keyboardInput{Type: inputKeyboard, WScan: uint16(scan), Flags: flags}
	w.injMu.Lock()
	if down {
		w.injKeys[vk] = scan
	} else {
		delete(w.injKeys, vk)
	}
	w.injMu.Unlock()
	sendInput(&inp)
}

// ReleaseAll 释放所有记录中的键鼠（防卡键）。
func (w *windowsInput) ReleaseAll() {
	w.injMu.Lock()
	type keyUp struct {
		scan uint32
	}
	ups := make([]keyUp, 0, len(w.injKeys))
	for _, scan := range w.injKeys {
		ups = append(ups, keyUp{scan})
	}
	w.injKeys = map[uint32]uint32{}
	buttons := make([]MouseButton, 0, len(w.injButtons))
	for b, down := range w.injButtons {
		if down {
			buttons = append(buttons, b)
		}
	}
	w.injButtons = map[MouseButton]bool{}
	w.injMu.Unlock()
	for _, k := range ups {
		inp := winInput{Type: inputKeyboard}
		inp.Keyboard = keyboardInput{Type: inputKeyboard, WScan: uint16(k.scan),
			Flags: keyeventfScanCode | keyeventfKeyUp}
		sendInput(&inp)
	}
	for _, b := range buttons {
		w.releaseButton(b)
	}
}

// releaseButton 单独发送按键抬起（不加锁版本）。
func (w *windowsInput) releaseButton(b MouseButton) {
	inp := winInput{Type: inputMouse}
	switch b {
	case ButtonLeft:
		inp.Mouse.Flags = mouseeventfLeftUp
	case ButtonRight:
		inp.Mouse.Flags = mouseeventfRightUp
	case ButtonMiddle:
		inp.Mouse.Flags = mouseeventfMiddleUp
	default:
		return
	}
	sendInput(&inp)
}

func sendInput(inp *winInput) {
	procSendInput.Call(1, uintptr(unsafe.Pointer(inp)), unsafe.Sizeof(*inp))
}

// mapVirtualKey VK → 扫描码兜底。
func mapVirtualKey(vk uint32) uint32 {
	r, _, _ := procMapVirtualKey.Call(uintptr(vk))
	return uint32(r)
}

var procMapVirtualKey = user32.NewProc("MapVirtualKeyW")

// parseHotkey 解析 "Ctrl+Alt+D" 形式的热键。
func parseHotkey(s string) (mods uint32, vk uint32, ok bool) {
	vk = 0xFFFFFFFF
	for _, part := range strings.Split(s, "+") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			mods |= modControl
		case "alt":
			mods |= modAlt
		case "shift":
			mods |= modShift
		default:
			if len(part) == 1 {
				c := part[0]
				if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
					vk = uint32(c)
				}
			}
		}
	}
	return mods, vk, vk != 0xFFFFFFFF && mods != 0
}

// NewPlatformProbe 诊断入口：暴露 NewPlatform 给最小复现程序。
func NewPlatformProbe() string {
	p := NewPlatform("Ctrl+Alt+D", log.Default())
	_ = p
	return "platform-ready"
}
