package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/agent"
	"github.com/wangxiuwen/winforge-agent/internal/client"
	"github.com/wangxiuwen/winforge-agent/internal/config"
	"github.com/wangxiuwen/winforge-agent/internal/security"
)

var version = "dev"

func main() {
	if handled, err := maybeRunAsService(); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "Windows 服务错误:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "pair":
		err = runPair(os.Args[2:])
	case "status":
		err = runStatus(os.Args[2:])
	case "mkdir":
		err = runMkdir(os.Args[2:])
	case "upload":
		err = runUpload(os.Args[2:])
	case "download":
		err = runDownload(os.Args[2:])
	case "exec":
		err = runExec(os.Args[2:])
	case "rotate-token":
		err = runRotateToken(os.Args[2:])
	case "service":
		err = runService(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("winforge", version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		err = fmt.Errorf("未知命令: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `WinForge Agent — 从 Mac/Linux 驱动 Windows 嵌入式构建机

Windows:
  winforge init [--config FILE] [--root DIR] [--listen ADDR]
  winforge serve [--config FILE]
  winforge service install|uninstall|start|stop|status [--config FILE]
  winforge rotate-token [--config FILE]

Mac/Linux:
  winforge pair NAME --host URL --token TOKEN --fingerprint SHA256
  winforge status --profile NAME
  winforge mkdir --profile NAME REMOTE_DIR
  winforge upload --profile NAME LOCAL REMOTE
  winforge download --profile NAME REMOTE LOCAL
  winforge exec --profile NAME [--cwd DIR] [--timeout 30m] -- COMMAND [ARG...]`)
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath(), "配置文件")
	root := fs.String("root", defaultRoot(), "workspace 根目录")
	listen := fs.String("listen", "0.0.0.0:9443", "监听地址")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*configPath); err == nil {
		return fmt.Errorf("配置已存在: %s；如需换 token 请用 rotate-token", *configPath)
	}
	dir := filepath.Dir(*configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(*root, 0o755); err != nil {
		return err
	}
	token, err := security.NewToken()
	if err != nil {
		return err
	}
	certFile := filepath.Join(dir, "agent.crt")
	keyFile := filepath.Join(dir, "agent.key")
	fingerprint, err := security.GenerateCertificate(certFile, keyFile, localHosts())
	if err != nil {
		return err
	}
	cfg := config.Config{
		Listen: *listen, Root: *root, Token: token,
		CertFile: certFile, KeyFile: keyFile,
		LogFile:        filepath.Join(dir, "agent.log"),
		MaxUploadBytes: 512 << 20, TimeoutText: "30m",
	}
	if err := config.Save(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("配置已创建: %s\nworkspace: %s\n证书指纹: %s\n", *configPath, *root, fingerprint)
	fmt.Printf("\n在 Mac 上通过可信通道执行：\nwinforge pair windows-lab --host https://<WINDOWS-IP>:%s --token %q --fingerprint %s\n", listenPort(*listen), token, fingerprint)
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath(), "配置文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return serveConfig(context.Background(), *configPath)
}

func serveConfig(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	logger := log.Default()
	var logFile *os.File
	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
			return err
		}
		logFile, err = os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer logFile.Close()
		logger = log.New(io.MultiWriter(os.Stderr, logFile), "", log.LstdFlags)
	}
	server, err := agent.New(cfg, logger)
	if err != nil {
		return err
	}
	return server.ListenAndServe(ctx)
}

func runPair(args []string) error {
	name := ""
	nameBeforeFlags := false
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		nameBeforeFlags = true
		args = args[1:]
	}
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	host := fs.String("host", "", "Agent HTTPS URL")
	token := fs.String("token", "", "配对 token")
	fingerprint := fs.String("fingerprint", "", "证书 SHA-256 指纹")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	unexpectedArgs := (nameBeforeFlags && fs.NArg() != 0) || (!nameBeforeFlags && fs.NArg() != 1)
	if name == "" || unexpectedArgs || *host == "" || *token == "" || *fingerprint == "" {
		return fmt.Errorf("用法: winforge pair NAME --host URL --token TOKEN --fingerprint SHA256")
	}
	profile := client.Profile{Host: *host, Token: *token, Fingerprint: *fingerprint}
	c, err := client.New(profile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Status(ctx, io.Discard); err != nil {
		return fmt.Errorf("配对验证失败: %w", err)
	}
	if err := client.SaveProfile(name, profile); err != nil {
		return err
	}
	fmt.Printf("已保存 profile %q 到 %s\n", name, client.ProfilesDir())
	return nil
}

func runStatus(args []string) error {
	profileName, rest, err := parseProfile(args)
	if err != nil || len(rest) != 0 {
		return firstError(err, fmt.Errorf("用法: winforge status --profile NAME"))
	}
	c, err := loadClient(profileName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.Status(ctx, os.Stdout)
}

func runMkdir(args []string) error {
	profileName, rest, err := parseProfile(args)
	if err != nil || len(rest) != 1 {
		return firstError(err, fmt.Errorf("用法: winforge mkdir --profile NAME REMOTE_DIR"))
	}
	c, err := loadClient(profileName)
	if err != nil {
		return err
	}
	ctx, cancel := client.DefaultContext()
	defer cancel()
	return c.Mkdir(ctx, rest[0])
}

func runUpload(args []string) error {
	profileName, rest, err := parseProfile(args)
	if err != nil || len(rest) != 2 {
		return firstError(err, fmt.Errorf("用法: winforge upload --profile NAME LOCAL REMOTE"))
	}
	c, err := loadClient(profileName)
	if err != nil {
		return err
	}
	ctx, cancel := client.DefaultContext()
	defer cancel()
	return c.Upload(ctx, rest[0], rest[1])
}

func runDownload(args []string) error {
	profileName, rest, err := parseProfile(args)
	if err != nil || len(rest) != 2 {
		return firstError(err, fmt.Errorf("用法: winforge download --profile NAME REMOTE LOCAL"))
	}
	c, err := loadClient(profileName)
	if err != nil {
		return err
	}
	ctx, cancel := client.DefaultContext()
	defer cancel()
	return c.Download(ctx, rest[0], rest[1])
}

func runExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile 名称")
	cwd := fs.String("cwd", ".", "远端工作目录")
	timeout := fs.Duration("timeout", 30*time.Minute, "命令超时")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := fs.Args()
	if *profileName == "" || len(command) == 0 {
		return fmt.Errorf("用法: winforge exec --profile NAME [--cwd DIR] -- COMMAND [ARG...]")
	}
	c, err := loadClient(*profileName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+time.Minute)
	defer cancel()
	exitCode, err := c.Exec(ctx, agent.ExecRequest{
		Command: command[0], Args: command[1:], Cwd: *cwd, TimeoutMS: timeout.Milliseconds(),
	}, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("远端退出码: %d", exitCode)
	}
	return nil
}

func runRotateToken(args []string) error {
	fs := flag.NewFlagSet("rotate-token", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath(), "配置文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	token, err := security.NewToken()
	if err != nil {
		return err
	}
	cfg.Token = token
	if err := config.Save(*configPath, cfg); err != nil {
		return err
	}
	fingerprint, err := security.CertificateFingerprint(cfg.CertFile)
	if err != nil {
		return err
	}
	fmt.Printf("token 已更新；重启 Agent 后生效。\ntoken: %s\nfingerprint: %s\n", token, fingerprint)
	return nil
}

func parseProfile(args []string) (string, []string, error) {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile 名称")
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	if *profile == "" {
		return "", nil, fmt.Errorf("缺少 --profile")
	}
	return *profile, fs.Args(), nil
}

func loadClient(name string) (*client.Client, error) {
	profile, err := client.LoadProfile(name)
	if err != nil {
		return nil, err
	}
	return client.New(profile)
}

func firstError(actual, fallback error) error {
	if actual != nil {
		return actual
	}
	return fallback
}

func defaultRoot() string {
	if os.Getenv("PROGRAMDATA") != "" {
		return filepath.Join(os.Getenv("PROGRAMDATA"), "WinForge", "workspaces")
	}
	return filepath.Join(filepath.Dir(config.DefaultPath()), "workspaces")
}

func localHosts() []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if host, err := os.Hostname(); err == nil {
		hosts = append(hosts, host)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ip := strings.Split(addr.String(), "/")[0]
			hosts = append(hosts, ip)
		}
	}
	return hosts
}

func listenPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		return port
	}
	return "9443"
}
