//go:build !windows

package main

import (
	"errors"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
)

var errDeskNoWindow = errors.New("桌面窗口当前仅支持 Windows，请使用浏览器控制台")

func runDeskUI(h *desk.Hub, startHidden bool) error { return errDeskNoWindow }

func deskShowConsole() {}
