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

// AutostartEnable 写入自启项。
func AutostartEnable() error {
	cmdline, err := AutostartCommand()
	if err != nil {
		return err
	}
	k, err := autostartKey()
	if err != nil {
		return fmt.Errorf("打开注册表 Run 键失败: %w", err)
	}
	defer k.Close()
	return k.SetStringValue(autostartValueName, cmdline)
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
