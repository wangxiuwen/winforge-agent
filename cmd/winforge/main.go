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
	"strconv"
	"strings"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/agent"
	"github.com/wangxiuwen/winforge-agent/internal/client"
	"github.com/wangxiuwen/winforge-agent/internal/config"
	"github.com/wangxiuwen/winforge-agent/internal/discovery"
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
	// 不带子命令 = 有人双击了它（或右键以管理员身份运行）。
	// Windows 上这时候跑一键安装，而不是甩一页 usage 让人自己拼命令。
	if len(os.Args) < 2 {
		if handled, err := runSetup(); handled {
			if err != nil {
				os.Exit(1)
			}
			return
		}
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "discover":
		err = runDiscover(os.Args[2:])
	case "pair-code":
		err = runPairCode(os.Args[2:])
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
  winforge pair-code [--config FILE]        生成 6 位配对码，念给对面即可
  winforge rotate-token [--config FILE]

Mac/Linux:
  winforge discover [--timeout 3s]
  winforge pair NAME (--host URL | --instance MDNS_NAME) --code 123456
  winforge pair NAME (--host URL | --instance MDNS_NAME) --token TOKEN --fingerprint SHA256   (老办法)
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
	fmt.Printf("\n下一步：先 winforge serve 起服务，再执行 winforge pair-code 拿一个 6 位配对码，\n" +
		"在 Mac 上跑：winforge pair windows-lab --instance <mDNS名> --code 123456\n\n" +
		"（也可以继续用长凭证手工配对，但要把下面两串都搬过去：）\n")
	fmt.Printf("winforge pair windows-lab --host https://<WINDOWS-IP>:%s --token %q --fingerprint %s\n", listenPort(*listen), token, fingerprint)
	return nil
}

// runPairCode 在 Agent 本机生成配对码。它连的是本机正在跑的服务：配对码必须存在
// 服务进程里才能被兑换，另起一个进程算出来的码没人认。
func runPairCode(args []string) error {
	fs := flag.NewFlagSet("pair-code", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath(), "配置文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fingerprint, err := security.CertificateFingerprint(cfg.CertFile)
	if err != nil {
		return fmt.Errorf("读取证书指纹失败: %w", err)
	}
	c, err := client.New(client.Profile{
		Host:        "https://127.0.0.1:" + listenPort(cfg.Listen),
		Token:       cfg.Token,
		Fingerprint: fingerprint,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	code, expires, err := c.ArmPairCode(ctx)
	if err != nil {
		return fmt.Errorf("生成配对码失败（Agent 服务在跑吗？winforge serve）: %w", err)
	}
	fmt.Printf("\n    配对码: %s\n\n", code)
	fmt.Printf("%s 之前有效，只能用一次。在 Mac 上执行：\n", expires.Local().Format("15:04:05"))
	fmt.Printf("  winforge pair windows-lab --instance <mDNS名> --code %s\n", code)
	fmt.Printf("\n（不知道 mDNS 名就先跑 winforge discover；也可以用 --host https://<本机IP>:%s）\n", listenPort(cfg.Listen))
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
	if cfg.AdvertiseEnabled() {
		advertiseCtx, stopAdvertise := context.WithCancel(ctx)
		defer stopAdvertise()
		go func() {
			if err := advertise(advertiseCtx, cfg, logger); err != nil && advertiseCtx.Err() == nil {
				logger.Printf("mDNS 公告已停止: %v", err)
			}
		}()
	}
	return server.ListenAndServe(ctx)
}

// advertise 只公告实例名、主机名、端口和证书指纹，不公告 token 或任何路径。
func advertise(ctx context.Context, cfg config.Config, logger *log.Logger) error {
	fingerprint, err := security.CertificateFingerprint(cfg.CertFile)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(listenPort(cfg.Listen))
	if err != nil {
		return err
	}
	instance := cfg.InstanceName
	if instance == "" {
		instance = discovery.DefaultInstanceName()
	}
	return discovery.Advertise(ctx, discovery.Service{
		Instance:    instance,
		Port:        port,
		Fingerprint: fingerprint,
	}, logger)
}

func runDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 3*time.Second, "搜索时长")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+5*time.Second)
	defer cancel()
	instances, err := discovery.Browse(ctx, *timeout)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		fmt.Println("未发现 Agent。确认 Agent 已启动、与本机同一网段，且没有被防火墙或 AP 隔离挡住多播。")
		return nil
	}
	for _, instance := range instances {
		fmt.Printf("%s\n", instance.Name)
		for _, candidate := range instance.URLs() {
			fmt.Printf("  host        %s\n", candidate)
		}
		if instance.Fingerprint != "" {
			fmt.Printf("  fingerprint %s\n", instance.Fingerprint)
		}
	}
	fmt.Println("\n公告里的指纹只是提示，配对时请使用 Agent init 输出的指纹。")
	return nil
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
	instance := fs.String("instance", "", "mDNS 实例名，用它代替固定 IP")
	token := fs.String("token", "", "配对 token（老办法；有 --code 就不用它）")
	fingerprint := fs.String("fingerprint", "", "证书 SHA-256 指纹（老办法；有 --code 就不用它）")
	code := fs.String("code", "", "6 位配对码，来自 Agent 上的 winforge pair-code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	unexpectedArgs := (nameBeforeFlags && fs.NArg() != 0) || (!nameBeforeFlags && fs.NArg() != 1)
	byCode := *code != ""
	if name == "" || unexpectedArgs || (*host == "" && *instance == "") ||
		(!byCode && (*token == "" || *fingerprint == "")) {
		return fmt.Errorf("用法: winforge pair NAME (--host URL | --instance MDNS_NAME) --code 123456\n" +
			"   或: winforge pair NAME (--host URL | --instance MDNS_NAME) --token TOKEN --fingerprint SHA256")
	}
	if byCode && (*token != "" || *fingerprint != "") {
		return fmt.Errorf("--code 和 --token/--fingerprint 是两种配对方式，不要混用")
	}
	profile := client.Profile{Host: *host, Token: *token, Fingerprint: *fingerprint, Instance: *instance}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if profile.Host == "" {
		// 用配对码时还没有指纹，此时的解析只为找地址；地址找错了后面 proof 也过不了。
		resolved, err := client.Relocate(ctx, profile, 4*time.Second)
		if err != nil {
			return err
		}
		profile = resolved
		fmt.Printf("已通过 mDNS 找到 %s\n", profile.Host)
	}
	if byCode {
		paired, err := client.PairWithCode(ctx, profile.Host, *code)
		if err != nil {
			return err
		}
		paired.Instance = profile.Instance
		profile = paired
	}
	c, err := client.New(profile)
	if err != nil {
		return err
	}
	if err := c.Status(ctx, io.Discard); err != nil {
		return fmt.Errorf("配对验证失败: %w", err)
	}
	if err := client.SaveProfile(name, profile); err != nil {
		return err
	}
	fmt.Printf("已保存 profile %q 到 %s\n", name, client.ProfilesDir())
	if profile.Instance == "" {
		fmt.Println("提示: 加 --instance 可在构建机重启换 IP 后自动重新定位。")
	}
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
	// 记了 mDNS 实例名时，Host 连不上会先在局域网里重新定位一次。
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.Open(ctx, name)
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
