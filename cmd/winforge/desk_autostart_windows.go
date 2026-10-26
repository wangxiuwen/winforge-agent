//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// autostart 通过 HKCU Run 键实现"开机自启"：
//   值名 WinForgeDesk，值为 "<exe路径>" desk --start-hidden
// 只写当前用户（HKCU），不需要管理员权限；窗口默认隐藏，只留托盘。

const autostartValueName = "WinForgeDesk"

func autostartKeyPath() registry.Key {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	return k
}

func autostartEnable() error {
	cmdline, err := autostartCommand()
	if err != nil {
		return err
	}
	k := autostartKeyPath()
	if k == 0 {
		return fmt.Errorf("打开注册表 Run 键失败")
	}
	defer k.Close()
	return k.SetStringValue(autostartValueName, cmdline)
}

func autostartDisable() error {
	k := autostartKeyPath()
	if k == 0 {
		return fmt.Errorf("打开注册表 Run 键失败")
	}
	defer k.Close()
	if err := k.DeleteValue(autostartValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// autostartStatus 返回 (是否启用, 命令行, 错误)。
func autostartStatus() (bool, string, error) {
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
