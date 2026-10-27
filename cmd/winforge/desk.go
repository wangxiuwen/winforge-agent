package main

import (
	"flag"
	"fmt"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
	"github.com/wangxiuwen/winforge-agent/internal/deskapp"
)

// winforge desk — desk 功能的 CLI 入口（浏览器控制台形态）。
// 桌面窗口（Wails + 托盘常驻）由独立入口 winforgedesk.exe 提供：
// webview2loader 会在包 init 阶段往 stdout 打调试行，不能进主 CLI。

func runDesk(args []string) error {
	// 子命令：winforge desk autostart on|off|status
	if len(args) > 0 && args[0] == "autostart" {
		return runDeskAutostart(args[1:])
	}
	fs := flag.NewFlagSet("desk", flag.ContinueOnError)
	configPath := fs.String("config", desk.DefaultConfigPath(), "配置文件路径")
	uiMode := fs.String("ui", "auto", "控制台形态: auto | browser（桌面窗口请用 winforgedesk.exe）")
	noTray := fs.Bool("no-tray", false, "不启动托盘（配合 --ui browser 做无头自测）")
	noOpen := fs.Bool("no-open", false, "浏览器模式不自动打开页面")
	name := fs.String("name", "", "显示名（默认主机名）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *uiMode == "window" || *uiMode == "auto" {
		// 主 CLI 无桌面窗口实现，一律浏览器控制台。
		*uiMode = "browser"
	}
	return deskapp.Run(deskapp.Options{
		ConfigPath: *configPath,
		UIMode:     *uiMode,
		NoTray:     *noTray,
		NoOpen:     *noOpen,
		Name:       *name,
	})
}

// runDeskAutostart 管理开机自启。
func runDeskAutostart(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "on":
		warn, err := deskapp.AutostartEnable()
		if err != nil {
			return err
		}
		if warn != "" {
			fmt.Println(warn)
		}
		cmdline, _ := deskapp.AutostartCommand()
		fmt.Println("已开启开机自启:", cmdline)
	case "off":
		if err := deskapp.AutostartDisable(); err != nil {
			return err
		}
		fmt.Println("已关闭开机自启")
	case "status":
		on, cmdline, err := deskapp.AutostartStatus()
		if err != nil {
			return err
		}
		if on {
			fmt.Println("开机自启: 开 —", cmdline)
		} else {
			fmt.Println("开机自启: 关")
		}
	default:
		return fmt.Errorf("未知 autostart 动作 %q（支持 on|off|status）", action)
	}
	return nil
}
