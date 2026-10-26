package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
)

// winforge desk — 多机键鼠共享托盘程序（共享 / 传输 / 控制）。
//
// Windows：默认起 Wails 桌面窗口 + 系统托盘；--ui browser 用浏览器控制台。
// 其他平台：浏览器控制台 + 托盘（键鼠控制暂不可用）。
func runDesk(args []string) error {
	fs := flag.NewFlagSet("desk", flag.ContinueOnError)
	configPath := fs.String("config", desk.DefaultConfigPath(), "配置文件路径")
	uiMode := fs.String("ui", "auto", "控制台形态: auto | window | browser")
	noTray := fs.Bool("no-tray", false, "不启动托盘（配合 --ui browser 做无头自测）")
	noOpen := fs.Bool("no-open", false, "浏览器模式不自动打开页面")
	name := fs.String("name", "", "显示名（默认主机名）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := desk.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *name != "" {
		cfg.Name = *name
	}
	logger := log.New(os.Stdout, "[desk] ", log.LstdFlags)
	logger.Printf("WinForge Desk 启动：名称=%s ID=%s 配置=%s", cfg.Name, cfg.ID, *configPath)

	platform := *desk.NewPlatform(cfg.Hotkey, logger)
	hub := desk.NewHub(cfg, *configPath, platform, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := hub.Start(ctx); err != nil {
		return err
	}

	useWindow := (*uiMode == "window") || (*uiMode == "auto" && runtime.GOOS == "windows")
	var browserURL string
	if !useWindow {
		addr, err := desk.StartBrowserUI(hub, cfg.UIPort)
		if err != nil {
			return fmt.Errorf("控制台启动失败: %w", err)
		}
		browserURL = "http://" + addr + "/?t=" + hub.Token()
		logger.Printf("控制台: %s", browserURL)
	}

	openConsole := func() {
		if useWindow {
			deskShowConsole()
			return
		}
		if browserURL != "" {
			openInBrowser(browserURL)
		}
	}
	onExit := func() {
		logger.Printf("退出")
		os.Exit(0)
	}

	if !*noOpen && browserURL != "" {
		openInBrowser(browserURL)
	}

	switch {
	case *noTray && useWindow:
		return runDeskUI(hub)
	case *noTray:
		<-ctx.Done()
		return nil
	case useWindow:
		go desk.RunTray(hub, openConsole, onExit)
		err := runDeskUI(hub)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	default:
		desk.RunTray(hub, openConsole, onExit)
		return nil
	}
}

func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
