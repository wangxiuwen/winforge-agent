//go:build windows

package deskapp

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// 开机自启：HKCU Run 键，值名 WinForgeDesk。只写当前用户，无需管理员权限。
// 命令行优先指向同目录的 winforgedesk.exe（GUI 入口），--start-hidden 登录后只留托盘。

const autostartValueName = "WinForgeDesk"

func autostartKey() (registry.Key, error) {
	return registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE|registry.QUERY_VALUE)
}

// AutostartCommand 返回要写进自启项的命令行。
func AutostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	gui := filepath.Join(filepath.Dir(exe), "winforgedesk.exe")
	if _, err := os.Stat(gui); err == nil {
		return `"` + gui + `" --start-hidden`, nil
	}
	return `"` + exe + `" desk --start-hidden`, nil
}

// AutostartEnable 写入自启项；返回覆盖警告（已有自启项且指向不同时非空，
// 提醒调用方展示——临时副本执行会把正式自启项改写到临时路径，R65 实踩）。
func AutostartEnable() (string, error) {
	cmdline, err := AutostartCommand()
	if err != nil {
		return "", err
	}
	k, err := autostartKey()
	if err != nil {
		return "", fmt.Errorf("打开注册表 Run 键失败: %w", err)
	}
	defer k.Close()
	warn := ""
	if old, _, err := k.GetStringValue(autostartValueName); err == nil && old != cmdline {
		warn = "注意：原自启项指向 " + old + "，已被覆盖"
	}
	if err := k.SetStringValue(autostartValueName, cmdline); err != nil {
		return "", err
	}
	return warn, nil
}

// AutostartDisable 删除自启项。
func AutostartDisable() error {
	k, err := autostartKey()
	if err != nil {
		return fmt.Errorf("打开注册表 Run 键失败: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(autostartValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// AutostartStatus 返回 (是否启用, 命令行, 错误)。
func AutostartStatus() (bool, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false, "", nil
	}
	defer k.Close()
	v, _, err := k.GetStringValue(autostartValueName)
	if err == registry.ErrNotExist {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, v, nil
}
