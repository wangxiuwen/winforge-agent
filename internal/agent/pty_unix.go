//go:build !windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

// startPTYSession 在 Unix 上起一条真 PTY 会话。agent 部署到 Linux/Mac 构建
// 机时，交互终端走的就是这条路径：/dev/ptmx 申请主从对，slave 给 shell 当
// 控制台，master 归会话读写。grant/unlock 与 slave 路径的取得方式随平台
// 不同，见 ptyname_<os>.go。
func startPTYSession(cols, rows uint16, cwd string) (shellSession, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	s := &unixSession{master: master}
	fail := func(err error) (shellSession, error) {
		master.Close()
		return nil, err
	}
	fd := int(master.Fd())
	slavePath, err := ptySlavePath(fd)
	if err != nil {
		return fail(err)
	}
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		return fail(fmt.Errorf("打开 %s: %w", slavePath, err))
	}
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols}); err != nil {
		slave.Close()
		return fail(fmt.Errorf("设置终端尺寸: %w", err))
	}

	shell := shellName()
	cmd := exec.Command(shell, "-i")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		slave.Close()
		return fail(fmt.Errorf("启动 %s: %w", shell, err))
	}
	slave.Close() // slave 已由子进程持有，父端用完即关
	s.cmd = cmd
	return s, nil
}

func shellName() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

type unixSession struct {
	master    *os.File
	cmd       *exec.Cmd
	closeOnce sync.Once
}

func (s *unixSession) Read(p []byte) (int, error)  { return s.master.Read(p) }
func (s *unixSession) Write(p []byte) (int, error) { return s.master.Write(p) }

func (s *unixSession) Resize(cols, rows uint16) error {
	return unix.IoctlSetWinsize(int(s.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
}

func (s *unixSession) Wait() (int, error) {
	if s.cmd == nil || s.cmd.Process == nil {
		return -1, fmt.Errorf("shell 进程未启动")
	}
	// 被信号杀死的 shell 会话是正常收尾路径（Close 之后），Wait 的 error
	// 不该盖过退出状态本身。
	waitErr := s.cmd.Wait()
	ps := s.cmd.ProcessState
	if ps == nil {
		return -1, waitErr
	}
	if ws, ok := ps.Sys().(unix.WaitStatus); ok {
		return ws.ExitStatus(), nil
	}
	return ps.ExitCode(), nil
}

// Close 断开 master：交互 shell 收到 SIGHUP 自行退出，仍活着就按进程组强杀。
func (s *unixSession) Close() error {
	s.closeOnce.Do(func() {
		s.master.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			terminateProcessTree(s.cmd)
		}
	})
	return nil
}
