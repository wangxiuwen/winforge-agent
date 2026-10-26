//go:build !windows

package main

import "errors"

var errAutostartUnsupported = errors.New("开机自启当前仅支持 Windows（可用系统会话脚本替代）")

func autostartEnable() error  { return errAutostartUnsupported }
func autostartDisable() error { return errAutostartUnsupported }

func autostartStatus() (bool, string, error) { return false, "", errAutostartUnsupported }
