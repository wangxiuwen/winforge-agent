//go:build windows

package agent

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

func shellName() string { return "powershell.exe" }

// startPTYSession 经 conpty-bridge 子命令起远端会话。ConPTY 的 conhost 继承
// 创建者的 std 形态，服务进程的 std 是空/文件时伪控制台静默失效——所以
// ConPTY 必须在被 exec 拉起（管道 std）的 bridge 进程里创建，本端只做
// stdio 帧桥，协议见 cmd/winforge/conpty_bridge_windows.go。
func startPTYSession(cols, rows uint16, cwd string) (shellSession, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "conpty-bridge",
		"--cols", fmt.Sprint(cols),
		"--rows", fmt.Sprint(rows),
		"--cwd", cwd,
		"--shell", shellCmdline(),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 conpty-bridge: %w", err)
	}

	s := &bridgeSession{cmd: cmd, stdin: stdin, stdout: stdout, stderr: bufio.NewReader(stderr), exitCh: make(chan int, 1)}
	// 等 started 事件（启动失败也在这里以 error 事件或进程退出表现）。
	line, err := s.stderr.ReadString('\n')
	var event struct {
		Event string `json:"event"`
		Code  int    `json:"code"`
		Msg   string `json:"msg"`
	}
	if err == nil {
		_ = json.Unmarshal([]byte(line), &event)
	}
	if event.Event == "error" || (err != nil && event.Event == "") {
		terminateProcessTree(cmd)
		msg := event.Msg
		if msg == "" {
			msg = "bridge 提前退出"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return s, nil
}

// shellCmdline：裸 powershell 直启。cmd /c "chcp 65001 & ..." 的套壳在
// ConPTY 下实测表现为 powershell 起来即退（退出码 1、零输出），UTF-8 输出
// 改由会话建立后注入一条 [Console]::OutputEncoding 设置命令解决。
func shellCmdline() string {
	return `powershell.exe -NoLogo`
}

type bridgeSession struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    *bufio.Reader
	exitCh    chan int
	writeOnce sync.Once
	closeOnce sync.Once
}

func (s *bridgeSession) Read(p []byte) (int, error) { return s.stdout.Read(p) }

func (s *bridgeSession) Write(b []byte) (int, error) {
	return s.writeFrame(0x01, b)
}

func (s *bridgeSession) Resize(cols, rows uint16) error {
	frame := make([]byte, 0, 5)
	frame = append(frame, 0x02)
	frame = binary.LittleEndian.AppendUint16(frame, cols)
	frame = binary.LittleEndian.AppendUint16(frame, rows)
	_, err := s.stdin.Write(frame)
	return err
}

func (s *bridgeSession) writeFrame(kind byte, b []byte) (int, error) {
	frame := make([]byte, 0, len(b)+3)
	frame = append(frame, kind)
	frame = binary.LittleEndian.AppendUint16(frame, uint16(len(b)))
	frame = append(frame, b...)
	_, err := s.stdin.Write(frame)
	return len(b), err
}

func (s *bridgeSession) Wait() (int, error) {
	// exit 事件在 stderr 上；bridge 退出也视为会话结束。
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		for {
			line, err := s.stderr.ReadString('\n')
			if err != nil {
				done <- result{code: -1}
				return
			}
			var event struct {
				Event string `json:"event"`
				Code  int    `json:"code"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Event == "exit" {
				done <- result{code: event.Code}
				return
			}
		}
	}()
	go func() {
		_ = s.cmd.Wait()
	}()
	select {
	case r := <-done:
		return r.code, r.err
	}
}

func (s *bridgeSession) Close() error {
	s.closeOnce.Do(func() {
		s.stdin.Close() // 关输入，bridge 收尾
		terminateProcessTree(s.cmd)
	})
	return nil
}
