//go:build windows

package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
)

// Wails 桌面窗口：同一套 web 前端，事件经 EventsEmit，命令走绑定方法。

var deskWailsCtx context.Context

// DeskApp 是绑定到前端的命令面（window.go.main.DeskApp.*）。
type DeskApp struct{ h *desk.Hub }

func (d *DeskApp) Snapshot() map[string]interface{} { return d.h.Snapshot() }
func (d *DeskApp) PairStart(addr string) error      { return d.h.PairStart(addr) }
func (d *DeskApp) PairRespond(reqID string, ok bool) {
	d.h.PairRespond(reqID, ok)
}
func (d *DeskApp) PairOutConfirm(ok bool) { d.h.PairConfirmOut(ok) }
func (d *DeskApp) PeerPosition(id, pos string) error {
	return d.h.PeerPosition(id, pos)
}
func (d *DeskApp) PeerToggle(id string, on bool) error { return d.h.PeerToggle(id, on) }
func (d *DeskApp) PeerRemove(id string) error          { return d.h.PeerRemove(id) }
func (d *DeskApp) SendChat(id, text string) error      { return d.h.SendChat(id, text) }
func (d *DeskApp) SendFile(id, path string) error      { return d.h.SendFile(id, path) }
func (d *DeskApp) TransferRespond(fid string, ok bool) { d.h.TransferRespond(fid, ok) }
func (d *DeskApp) CancelTransfer(fid string)           { d.h.CancelTransfer(fid) }
func (d *DeskApp) ChatHistory(peerID string) []desk.ChatMsg {
	return d.h.ChatHistory(peerID)
}
func (d *DeskApp) ClearChat(peerID string)     { d.h.DeleteChatHistory(peerID) }
func (d *DeskApp) TakeControl(id string) error { return d.h.TakeControl(id, -1) }
func (d *DeskApp) ReleaseControl()             { d.h.ReleaseControl(-1) }
func (d *DeskApp) ToggleShare(on bool)         { d.h.ToggleShare(on) }
func (d *DeskApp) OpenPath(path string) error  { return d.h.OpenPath(path) }

// PickAndSendFile 弹系统文件选择框并发送。
func (d *DeskApp) PickAndSendFile(peerID string) error {
	if deskWailsCtx == nil {
		return errors.New("窗口未就绪")
	}
	path, err := wruntime.OpenFileDialog(deskWailsCtx, wruntime.OpenDialogOptions{
		Title: "选择要发送的文件",
	})
	if err != nil || path == "" {
		return nil
	}
	return d.h.SendFile(peerID, path)
}

type deskWailsEmitter struct{}

func (deskWailsEmitter) Emit(event string, data interface{}) {
	if deskWailsCtx != nil {
		wruntime.EventsEmit(deskWailsCtx, event, data)
	}
}

func runWindowUI(h *desk.Hub, startHidden bool) error {
	return wails.Run(&options.App{
		Title:            "WinForge Desk",
		Width:            1100,
		Height:           760,
		MinWidth:         860,
		MinHeight:        560,
		StartHidden:      startHidden,
		BackgroundColour: options.NewRGB(16, 20, 26),
		AssetServer: &assetserver.Options{
			Assets: desk.WebFS(),
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
			CSSDropProperty:    "--wails-drop-target",
		},
		OnStartup: func(ctx context.Context) {
			deskWailsCtx = ctx
			h.AddEmitter(deskWailsEmitter{})
			// 拖放文件 → 交给前端按当前选中的对端发送。
			wruntime.OnFileDrop(ctx, func(_ int, _ int, paths []string) {
				wruntime.EventsEmit(ctx, "desk:filedrop", paths)
			})
		},
		OnBeforeClose: func(ctx context.Context) bool {
			// 点关闭只隐藏，托盘常驻；退出走托盘菜单。
			wruntime.WindowHide(ctx)
			return true
		},
		Bind: []interface{}{&DeskApp{h}},
	})
}

func deskShowConsole() {
	if deskWailsCtx != nil {
		wruntime.WindowShow(deskWailsCtx)
	}
}
