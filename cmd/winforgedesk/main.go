// winforgedesk — WinForge Desk 的桌面入口：Wails 窗口 + 系统托盘常驻。
//
// 独立于 winforge CLI 存在的原因：Wails 的 webview2loader 在包 init 阶段
// 向 stdout 输出调试行，且 GUI 版需要 -H=windowsgui（无控制台黑窗）。
// 本入口把日志写到 %LOCALAPPDATA%\winforge\desk.log 便于排查“双击没反应”。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
	"github.com/wangxiuwen/winforge-agent/internal/deskapp"
)

// version 由 Makefile 的 -ldflags "-X main.version=..." 注入。
var version = "dev"

func main() {
	// 兼容 CLI 语法 `winforgedesk desk autostart ...`（与 `winforge desk
	// autostart` 对齐）：不兼容时该写法会静默回落成启动完整 hub——多实例
	// 抢端口（R65 实踩）。
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "desk" && args[1] == "autostart" {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "autostart" {
		if err := runAutostart(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		return
	}
	fs := flag.NewFlagSet("winforgedesk", flag.ContinueOnError)
	configPath := fs.String("config", desk.DefaultConfigPath(), "配置文件路径")
	uiMode := fs.String("ui", "auto", "auto | browser（排障时强制浏览器控制台）")
	noTray := fs.Bool("no-tray", false, "不启动托盘")
	noOpen := fs.Bool("no-open", false, "浏览器模式不自动打开页面")
	startHidden := fs.Bool("start-hidden", false, "启动时隐藏主窗口（开机自启用）")
	name := fs.String("name", "", "显示名（默认主机名）")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	logWriter, closeLog, logErr := deskapp.OpenRotatingLog(*configPath)
	if logErr != nil {
		logWriter = os.Stderr
	} else {
		defer closeLog()
	}

	err := deskapp.Run(deskapp.Options{
		Version:     version,
		ConfigPath:  *configPath,
		UIMode:      *uiMode,
		NoTray:      *noTray,
		NoOpen:      *noOpen,
		StartHidden: *startHidden,
		Name:        *name,
		LogWriter:   logWriter,
		WindowUI:    runWindowUI,
		WindowShow:  deskShowConsole,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

// runAutostart GUI 入口也提供 autostart（写自启项时优先指向自己）。
func runAutostart(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "on":
		if err := deskapp.AutostartEnable(); err != nil {
			return err
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
