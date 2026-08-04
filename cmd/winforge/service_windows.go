//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/config"
	"golang.org/x/sys/windows/svc"
)

const serviceName = "WinForgeAgent"

func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: winforge service install|uninstall|start|stop|status [--config FILE]")
	}
	action := args[0]
	configPath := config.DefaultPath()
	for i := 1; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		} else {
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch action {
	case "install":
		if _, err := config.Load(configPath); err != nil {
			return fmt.Errorf("先运行 init 创建有效配置: %w", err)
		}
		installDir := filepath.Dir(configPath)
		installedExe := filepath.Join(installDir, "winforge.exe")
		if !strings.EqualFold(exe, installedExe) {
			if err := copyExecutable(exe, installedExe); err != nil {
				return err
			}
		}
		binPath := fmt.Sprintf(`"%s" serve --config "%s"`, installedExe, configPath)
		return runSC("create", serviceName, "binPath=", binPath, "start=", "auto", "obj=", `NT AUTHORITY\LocalService`, "DisplayName=", "WinForge Agent")
	case "uninstall":
		_ = runSC("stop", serviceName)
		return runSC("delete", serviceName)
	case "start", "stop":
		return runSC(action, serviceName)
	case "status":
		return runSC("query", serviceName)
	default:
		return fmt.Errorf("未知 service 操作: %s", action)
	}
}

func maybeRunAsService() (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false, err
	}
	configPath := config.DefaultPath()
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--config" && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			i++
		}
	}
	return true, svc.Run(serviceName, &serviceHandler{configPath: configPath})
}

type serviceHandler struct{ configPath string }

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveConfig(ctx, h.configPath) }()
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case change := <-requests:
			switch change.Cmd {
			case svc.Interrogate:
				statuses <- change.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						return true, 1
					}
				case <-time.After(20 * time.Second):
					return true, 1
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		}
	}
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".new"
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Rename(tmp, target)
}

func runSC(args ...string) error {
	cmd := exec.Command("sc.exe", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sc.exe %s: %w（请确认已用管理员终端运行）", strings.Join(args, " "), err)
	}
	return nil
}
