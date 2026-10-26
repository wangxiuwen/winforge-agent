//go:build windows

package desk

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 控制状态边缘提示条：共享激活期间在虚拟屏幕对应侧显示一条 3px 常亮色条
// （我在控制=绿、被控制=琥珀），空闲隐藏。窗口为分层+鼠标穿透+置顶+工具窗，
// 不抢焦点、不挡点击，纯可视化。真机视觉效果待用户实测。

var (
	edgeGdi32               = windows.NewLazySystemDLL("gdi32.dll")
	edgeUser32              = windows.NewLazySystemDLL("user32.dll")
	procEdgeGetModuleHandle = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procEdgeRegisterClassEx = edgeUser32.NewProc("RegisterClassExW")
	procEdgeCreateWindowEx  = edgeUser32.NewProc("CreateWindowExW")
	procEdgeDefWindowProc   = edgeUser32.NewProc("DefWindowProcW")
	procEdgeSetLayered      = edgeUser32.NewProc("SetLayeredWindowAttributes")
	procEdgeSetWindowPos    = edgeUser32.NewProc("SetWindowPos")
	procEdgeShowWindow      = edgeUser32.NewProc("ShowWindow")
	procEdgeInvalidateRect  = edgeUser32.NewProc("InvalidateRect")
	procEdgeCreateSolidBr   = edgeGdi32.NewProc("CreateSolidBrush")
	procEdgeSetClassLongPtr = edgeUser32.NewProc("SetClassLongPtrW")
	procEdgeGetMessage      = edgeUser32.NewProc("GetMessageW")
	procEdgeLoadCursor      = edgeUser32.NewProc("LoadCursorW")
)

const (
	edgeWSExLayered     = 0x00080000
	edgeWSExTopmost     = 0x00000008
	edgeWSExTransparent = 0x00000020
	edgeWSExToolWindow  = 0x00000080
	edgeWSPopup         = 0x80000000

	edgeSWHide       = 0
	edgeSWShowNoAct  = 4
	edgeHWNDTopmost  = ^uintptr(0) // -1
	edgeSWPNoAct     = 0x0010
	edgeSWPShow      = 0x0040
	edgeLWAAlpha     = 0x02
	edgeGCLPHbrBrush = ^uintptr(9) // GCLP_HBRBACKGROUND(-10)

	edgeIDCArrow = 32512

	// COLORREF 是 0x00BBGGRR。
	edgeColorController = 0x0052C452 // R=82  G=196 B=122 绿
	edgeColorControlled = 0x003DA3E8 // R=232 G=163 B=61  琥珀
)

type edgeWndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type edgeBarCmd struct {
	on         bool
	controller bool
	edge       Edge
	bounds     ScreenBounds
}

const edgeBarThickness = 3

// startEdgeIndicator 启动提示条常驻线程（消息循环），返回线程安全的状态投递函数。
// 进程退出即销毁，无需显式停止。
func startEdgeIndicator() (func(on bool, controller bool, edge Edge), error) {
	cmdCh := make(chan edgeBarCmd, 8)
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defProc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
			r, _, _ := procEdgeDefWindowProc.Call(hwnd, msg, wp, lp)
			return r
		})
		hInst, _, _ := procEdgeGetModuleHandle.Call(0)
		className, _ := syscall.UTF16PtrFromString("WinForgeDeskEdgeBar")
		green, _, _ := procEdgeCreateSolidBr.Call(edgeColorController)
		wc := edgeWndClassExW{
			CbSize:        uint32(unsafe.Sizeof(edgeWndClassExW{})),
			LpfnWndProc:   defProc,
			HInstance:     syscall.Handle(hInst),
			HCursor:       syscall.Handle(edgeLoadArrow()),
			HbrBackground: syscall.Handle(green),
			LpszClassName: className,
		}
		atom, _, err := procEdgeRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			ready <- err
			return
		}
		hwnd, _, err := procEdgeCreateWindowEx.Call(
			edgeWSExLayered|edgeWSExTopmost|edgeWSExTransparent|edgeWSExToolWindow,
			uintptr(unsafe.Pointer(className)), 0, edgeWSPopup,
			0, 0, 0, 0, 0, 0, hInst, 0)
		if hwnd == 0 {
			ready <- err
			return
		}
		procEdgeSetLayered.Call(hwnd, 0, 0, edgeLWAAlpha) // 初始全透明，由渐显接管
		ready <- nil

		fadeTo := func(target byte) {
			for alpha := byte(0); alpha <= target; alpha += 24 {
				procEdgeSetLayered.Call(hwnd, 0, uintptr(alpha), edgeLWAAlpha)
				time.Sleep(16 * time.Millisecond) // 约一帧
			}
			procEdgeSetLayered.Call(hwnd, 0, uintptr(target), edgeLWAAlpha)
		}
		for cmd := range cmdCh {
			if !cmd.on {
				// 渐隐后隐藏；阻塞条线程最长 ~200ms，可接受。
				for alpha := int(170); alpha > 0; alpha -= 34 {
					procEdgeSetLayered.Call(hwnd, 0, uintptr(alpha), edgeLWAAlpha)
					time.Sleep(16 * time.Millisecond)
				}
				procEdgeShowWindow.Call(hwnd, edgeSWHide)
				continue
			}
			var color uintptr = edgeColorController
			if !cmd.controller {
				color = edgeColorControlled
			}
			brush, _, _ := procEdgeCreateSolidBr.Call(color)
			procEdgeSetClassLongPtr.Call(hwnd, edgeGCLPHbrBrush, brush)
			procEdgeInvalidateRect.Call(hwnd, 0, 1)
			x, y, w, h := edgeBarRect(cmd.edge, cmd.bounds)
			procEdgeSetWindowPos.Call(hwnd, edgeHWNDTopmost,
				uintptr(x), uintptr(y), uintptr(w), uintptr(h),
				edgeSWPNoAct|edgeSWPShow)
			fadeTo(170)
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return func(on bool, controller bool, edge Edge) {
		select {
		case cmdCh <- edgeBarCmd{on: on, controller: controller, edge: edge, bounds: virtualScreenBounds()}:
		default:
		}
	}, nil
}

// edgeBarRect 计算某侧提示条的位置与尺寸（虚拟桌面坐标）。
func edgeBarRect(edge Edge, b ScreenBounds) (x, y, w, h int32) {
	t := int32(edgeBarThickness)
	switch edge {
	case EdgeLeft:
		return b.X, b.Y, t, b.H
	case EdgeRight:
		return b.X + b.W - t, b.Y, t, b.H
	case EdgeTop:
		return b.X, b.Y, b.W, t
	case EdgeBottom:
		return b.X, b.Y + b.H - t, b.W, t
	}
	return b.X, b.Y, t, t
}

func edgeLoadArrow() uintptr {
	r, _, _ := procEdgeLoadCursor.Call(0, uintptr(edgeIDCArrow))
	return r
}

// virtualScreenBounds 当前虚拟桌面范围。
func virtualScreenBounds() ScreenBounds {
	x, _, _ := procEdgeGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procEdgeGetSystemMetrics.Call(smYVirtualScreen)
	cx, _, _ := procEdgeGetSystemMetrics.Call(smCXVirtualScreen)
	cy, _, _ := procEdgeGetSystemMetrics.Call(smCYVirtualScreen)
	return ScreenBounds{X: int32(x), Y: int32(y), W: int32(cx), H: int32(cy)}
}

var procEdgeGetSystemMetrics = edgeUser32.NewProc("GetSystemMetrics")
