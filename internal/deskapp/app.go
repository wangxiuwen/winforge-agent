// Package deskapp 承载 `winforge desk` / winforgedesk 的运行体。
//
// 本包不 import Wails：桌面窗口由 GUI 入口（cmd/winforgedesk）通过
// Options.WindowUI 注入，从而保证构建 Agent 的主 CLI 不加载 Wails
// （webview2loader 会在包 init 阶段往 stdout 打调试行，污染 CLI 输出）。
package deskapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/wangxiuwen/winforge-agent/internal/desk"
)

// Options 是一次 desk 运行的入参。
type Options struct {
	// ConfigPath 配置文件路径；为空用 desk.DefaultConfigPath()。
	ConfigPath string
	// UIMode: auto | window | browser。"window" 仅在 WindowUI 非空时生效。
	UIMode string
	// NoTray 不启动托盘（无头自测用）。
	NoTray bool
	// NoOpen 浏览器模式不自动打开页面。
	NoOpen bool
	// StartHidden 桌面窗口启动时隐藏（开机自启场景）。
	StartHidden bool
	// Name 覆盖显示名。
	Name string
	// LogWriter 日志去处；为空用 os.Stdout（GUI 入口应传日志文件）。
	LogWriter io.Writer
	// WindowUI 由 GUI 入口注入的桌面窗口实现（Windows: Wails）；可为 nil。
	WindowUI func(h *desk.Hub, startHidden bool) error
	// WindowShow 唤起已隐藏的主窗口（托盘“打开控制台”用）；可为 nil。
	WindowShow func()
}

// Run 启动 desk 直到退出；errDeskUsage 之外的错误已带上下文。
func Run(opts Options) error {
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = desk.DefaultConfigPath()
	}
	uiMode := opts.UIMode
	if uiMode == "" {
		uiMode = "auto"
	}
	loggerOut := opts.LogWriter
	if loggerOut == nil {
		loggerOut = os.Stdout
	}

	cfg, err := desk.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	if opts.Name != "" {
		cfg.Name = opts.Name
	}
	logger := log.New(loggerOut, "[desk] ", log.LstdFlags)
	logger.Printf("WinForge Desk 启动：名称=%s ID=%s 配置=%s", cfg.Name, cfg.ID, cfgPath)

	platform := *desk.NewPlatform(cfg.Hotkey, logger)
	hub := desk.NewHub(cfg, cfgPath, platform, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := hub.Start(ctx); err != nil {
		return err
	}

	useWindow := opts.WindowUI != nil &&
		(uiMode == "window" || (uiMode == "auto" && runtime.GOOS == "windows"))
	if uiMode == "window" && opts.WindowUI == nil {
		logger.Printf("此可执行文件不含桌面窗口（用 winforgedesk.exe），回退浏览器控制台")
	}
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
			if opts.WindowShow != nil {
				opts.WindowShow()
			}
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

	if !opts.NoOpen && browserURL != "" {
		openInBrowser(browserURL)
	}

	switch {
	case opts.NoTray && useWindow:
		return opts.WindowUI(hub, opts.StartHidden)
	case opts.NoTray:
		<-ctx.Done()
		return nil
	case useWindow:
		go desk.RunTray(hub, openConsole, onExit)
		err := opts.WindowUI(hub, opts.StartHidden)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	default:
		desk.RunTray(hub, openConsole, onExit)
		return nil
	}
}

// WindowShow 是 GUI 入口可选注入的“唤起窗口”回调（托盘“打开控制台”用）。

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
