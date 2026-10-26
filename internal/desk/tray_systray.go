//go:build windows || linux

package desk

import (
	"fmt"

	"fyne.io/systray"
)

// 系统托盘：打开控制台 / 共享开关 / 退出。
// 调用方决定 RunTray 跑在主协程还是后台协程（macOS 要求主协程）。

type trayEmitter struct {
	share *systray.MenuItem
}

func (t trayEmitter) Emit(event string, data interface{}) {
	switch event {
	case "desk:state":
		if m, ok := data.(map[string]interface{}); ok {
			if share, ok := m["share"].(bool); ok {
				if share {
					t.share.Check()
					t.share.SetTitle("键鼠共享（开）")
				} else {
					t.share.Uncheck()
					t.share.SetTitle("键鼠共享（关）")
				}
			}
			if ctl, ok := m["control"].(ControlState); ok {
				tip := "WinForge Desk — 共享 / 传输 / 控制"
				switch ctl.Role {
				case roleController:
					tip += fmt.Sprintf("（正在控制 %s）", ctl.Peer)
				case roleControlled:
					tip += fmt.Sprintf("（被 %s 控制）", ctl.Peer)
				}
				systray.SetTooltip(tip)
			}
		}
	}
}

// RunTray 启动托盘并阻塞。
func RunTray(h *Hub, openConsole func(), onExit func()) {
	systray.Run(func() {
		systray.SetIcon(TrayIcon)
		systray.SetTitle("WinForge Desk")
		systray.SetTooltip("WinForge Desk — 共享 / 传输 / 控制")
		mOpen := systray.AddMenuItem("打开控制台", "打开控制台")
		mShare := systray.AddMenuItemCheckbox("键鼠共享", "共享总开关", h.ShareEnabledSnapshot())
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 WinForge Desk")
		h.AddEmitter(trayEmitter{share: mShare})
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openConsole()
				case <-mShare.ClickedCh:
					h.ToggleShare(!h.ShareEnabledSnapshot())
				case <-mQuit.ClickedCh:
					onExit()
					return
				}
			}
		}()
	}, nil)
}

// ShareEnabledSnapshot 并发安全的共享开关读取。
func (h *Hub) ShareEnabledSnapshot() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ShareEnabled
}
