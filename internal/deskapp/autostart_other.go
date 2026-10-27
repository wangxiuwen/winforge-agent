//go:build !windows

package deskapp

import (
	"errors"
	"os"
)

var ErrAutostartUnsupported = errors.New("开机自启当前仅支持 Windows（可用系统会话脚本替代）")

func AutostartEnable() (string, error) { return "", ErrAutostartUnsupported }
func AutostartDisable() error { return ErrAutostartUnsupported }

func AutostartStatus() (bool, string, error) { return false, "", ErrAutostartUnsupported }

// AutostartCommand 非 Windows 平台的占位实现（自启本身不可用）。
func AutostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" desk --start-hidden`, nil
}
