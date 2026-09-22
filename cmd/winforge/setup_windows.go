//go:build windows

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/config"
	"golang.org/x/sys/windows"
)

// runSetup 是"右键以管理员身份运行"这一条路径。
//
// 双击一个 exe 的人不会去背四条命令，而这四条少一条都不工作：init 建配置、
// service install 装服务、service start 起来、pair-code 拿配对码。所以不带
// 任何参数运行时，就把这四件事按顺序做完，最后把配对码和连接地址一起打出来。
//
// 全程幂等：配置已存在就跳过 init，服务已存在就跳过 create，已经在跑就不再
// start —— 装到一半失败再点一次能接着走完，这正是双击场景下最常发生的事。
//
// 注意不要和服务自身启动搞混：SCM 拉起来时也是"没有子命令"的，那条路在
// maybeRunAsService() 里已经用 svc.IsWindowsService() 分流了，走不到这里。
func runSetup() (bool, error) {
	fmt.Println("WinForge Agent —— 一键安装为 Windows 服务")
	fmt.Println(strings.Repeat("=", 52))
	err := setupSteps()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n安装失败:", err)
	}
	// 双击起来的窗口在进程退出时会立刻消失，不停一下人什么都看不到 ——
	// 包括最关键的那 6 位配对码。
	fmt.Print("\n按回车键退出...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	return true, err
}

func setupSteps() error {
	if !isElevated() {
		return fmt.Errorf("需要管理员权限：请右键这个程序，选「以管理员身份运行」")
	}
	configPath := config.DefaultPath()

	// 1/4 配置
	if _, statErr := os.Stat(configPath); statErr == nil {
		fmt.Printf("[1/4] 配置已存在，跳过: %s\n", configPath)
	} else {
		fmt.Printf("[1/4] 创建配置: %s\n", configPath)
		if err := runInit(nil); err != nil {
			return err
		}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// 2/4 服务
	if serviceExists() {
		fmt.Println("[2/4] 服务已安装，跳过")
	} else {
		fmt.Println("[2/4] 安装为 Windows 服务")
		if err := runService([]string{"install", "--config", configPath}); err != nil {
			return err
		}
	}
	// 服务跑在 LocalService 账号下，而 workspace 建的时候是当前管理员的权限。
	// 不补这条 ACL，服务起来了但一上传文件就 Access Denied，而且报在客户端，
	// 现场看着像网络问题。
	grantWorkspaceAccess(cfg.Root)

	// 3/4 启动
	if serviceRunning() {
		fmt.Println("[3/4] 服务已在运行")
	} else {
		fmt.Println("[3/4] 启动服务")
		if err := runService([]string{"start"}); err != nil {
			return err
		}
	}
	if err := waitForPort(listenPort(cfg.Listen), 20*time.Second); err != nil {
		return fmt.Errorf("服务起来了但端口没监听: %w", err)
	}
	openFirewall(listenPort(cfg.Listen))

	// 4/4 配对码
	fmt.Println("[4/4] 生成配对码")
	fmt.Println()
	if err := runPairCode([]string{"--config", configPath}); err != nil {
		return err
	}
	printReachableAddresses(listenPort(cfg.Listen))
	return nil
}

func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// scQuiet 跑 sc.exe 但不把输出糊到屏幕上：这里只关心"存在吗/在跑吗"。
func scQuiet(args ...string) (string, error) {
	out, err := exec.Command("sc.exe", args...).CombinedOutput()
	return string(out), err
}

func serviceExists() bool {
	_, err := scQuiet("query", serviceName)
	return err == nil
}

func serviceRunning() bool {
	out, err := scQuiet("query", serviceName)
	return err == nil && strings.Contains(out, "RUNNING")
}

func grantWorkspaceAccess(root string) {
	if root == "" {
		return
	}
	// /T 递归，(OI)(CI)F 让新建的子项也继承；失败不致命，只提示。
	if out, err := exec.Command("icacls.exe", root, "/grant", `NT AUTHORITY\LocalService:(OI)(CI)F`, "/T", "/C").CombinedOutput(); err != nil {
		fmt.Printf("      提示: 给 workspace 授权失败，上传可能会 Access Denied: %v %s\n", err, strings.TrimSpace(string(out)))
	}
}

// openFirewall 放行入站端口。
//
// Windows 防火墙默认挡掉新监听程序的入站连接，而且**连 ping 都不通**，
// 所以对面看到的现象是"这台机器整个连不上"，很容易被当成网线/网段/VPN
// 的问题去查。装完服务不开这个口子，等于没装。
//
// 幂等：同名规则已存在就不再加，否则每点一次安装就多一条重复规则。
func openFirewall(port string) {
	name := "WinForge Agent"
	if out, err := exec.Command("netsh.exe", "advfirewall", "firewall", "show", "rule",
		"name="+name).CombinedOutput(); err == nil && strings.Contains(string(out), name) {
		fmt.Println("      防火墙规则已存在，跳过")
		return
	}
	if out, err := exec.Command("netsh.exe", "advfirewall", "firewall", "add", "rule",
		"name="+name, "dir=in", "action=allow", "protocol=TCP",
		"localport="+port).CombinedOutput(); err != nil {
		fmt.Printf("      提示: 防火墙放行失败，对面可能连不上 %s 端口: %v %s\n",
			port, err, strings.TrimSpace(string(out)))
		return
	}
	fmt.Printf("      已放行入站 TCP %s\n", port)
}

func removeFirewall() {
	_ = exec.Command("netsh.exe", "advfirewall", "firewall", "delete", "rule",
		"name=WinForge Agent").Run()
}

func waitForPort(port string, within time.Duration) error {
	deadline := time.Now().Add(within)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

// printReachableAddresses 把对面该填什么直接打出来。
// 装完之后人第一个要问的就是"那我在 Mac 上连哪个地址"，让他自己去
// ipconfig 里挑一个网卡是没必要的一步。
func printReachableAddresses(port string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	var hosts []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		hosts = append(hosts, ipNet.IP.String())
	}
	if len(hosts) == 0 {
		return
	}
	fmt.Println("\n这台机器的地址（Mac 那边 --host 用它）:")
	for _, host := range hosts {
		fmt.Printf("  https://%s:%s\n", host, port)
	}
}
