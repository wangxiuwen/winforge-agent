//go:build !windows

package desk

import (
	"errors"
	"log"
)

// 非 Windows 平台：键鼠捕获/注入暂未实现（Wails 界面、配对、消息、
// 文件传输均可用）。构建不失败，控制类操作返回明确错误。

// NewPlatform 返回占位实现。
func NewPlatform(hotkey string, logger *log.Logger) *Platform {
	return &Platform{
		Injector: func() (Injector, error) {
			return nil, errors.New("键鼠控制当前仅支持 Windows")
		},
		StartCapture: func(chan InputEvent) (func(), error) {
			return nil, errors.New("键鼠控制当前仅支持 Windows")
		},
		ScreenBounds: func() ScreenBounds { return ScreenBounds{} },
		CursorPos:    func() (int32, int32) { return 0, 0 },
		SetCursor:    func(int32, int32) {},
	}
}
