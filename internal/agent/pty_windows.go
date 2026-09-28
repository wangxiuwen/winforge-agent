//go:build windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ConPTY（Windows 伪控制台）绑定，只取交互终端用到的最小面。
// 参考 cardinality: CreatePseudoConsole/Resize/Close + 挂
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE 的 CreateProcessW。
var (
	procCreatePseudoConsole       = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole       = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole        = kernel32.NewProc("ClosePseudoConsole")
	procInitProcThreadAttrList    = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttrList  = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW            = kernel32.NewProc("CreateProcessW")
)

const (
	procThreadAttributePseudoConsole = 0x00020016
	extendedStartupInfoPresent       = 0x00080000
	createUnicodeEnvironment         = 0x00000400
)

type processInformation struct {
	Process   syscall.Handle
	Thread    syscall.Handle
	ProcessID uint32
	ThreadID  uint32
}

type startupInfoExW struct {
	syscall.StartupInfo
	AttributeList *byte
}

func shellName() string { return "powershell.exe" }

// startPTYSession 起一条 ConPTY 会话。shell 经 `cmd /c chcp 65001 & powershell`
// 启动：ConPTY 新控制台的输出代码页默认是 OEM（中文系统 936），先切 65001
// 再进 PowerShell，终端流才是 UTF-8。
func startPTYSession(cols, rows uint16, cwd string) (shellSession, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	s := &conptySession{inW: inW, outR: outR}

	var hpc uintptr
	hr, _, callErr := procCreatePseudoConsole.Call(
		uintptr(cols)|uintptr(rows)<<16,
		inR.Fd(), outW.Fd(), 0, uintptr(unsafe.Pointer(&hpc)))
	if hr != 0 {
		s.inW.Close()
		s.outR.Close()
		return nil, fmt.Errorf("CreatePseudoConsole: %v", callErr)
	}
	s.hpc = hpc

	// 伪控制台的尺寸已随创建给出；管道句柄已被 ConPTY 复制，父端用完即关。
	inR.Close()
	outW.Close()

	attr, err := s.spawnShell(cwd)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.attr = attr
	return s, nil
}

func (s *conptySession) spawnShell(cwd string) (*byte, error) {
	var attrSize uintptr
	hr, _, callErr := procInitProcThreadAttrList.Call(0, 1, 0, uintptr(unsafe.Pointer(&attrSize)))
	if hr == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList(size): %v", callErr)
	}
	attr := make([]byte, attrSize)
	hr, _, callErr = procInitProcThreadAttrList.Call(
		uintptr(unsafe.Pointer(&attr[0])), 1, 0, uintptr(unsafe.Pointer(&attrSize)))
	if hr == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList: %v", callErr)
	}
	hr, _, callErr = procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(&attr[0])), 0, procThreadAttributePseudoConsole,
		s.hpc, unsafe.Sizeof(s.hpc), 0, 0)
	if hr == 0 {
		procDeleteProcThreadAttrList.Call(uintptr(unsafe.Pointer(&attr[0])))
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %v", callErr)
	}

	cmdline, err := syscall.UTF16PtrFromString(`cmd.exe /c "chcp 65001 >nul & powershell.exe -NoLogo"`)
	if err != nil {
		procDeleteProcThreadAttrList.Call(uintptr(unsafe.Pointer(&attr[0])))
		return nil, err
	}
	cwdPtr, err := syscall.UTF16PtrFromString(cwd)
	if err != nil {
		procDeleteProcThreadAttrList.Call(uintptr(unsafe.Pointer(&attr[0])))
		return nil, err
	}
	var si startupInfoExW
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi processInformation
	// 伪控制台场景 bInheritHandles 必须为 FALSE：管道由 ConPTY 侧持有，
	// 让子进程继承反而把没有伪控制台的句柄也漏过去。
	hr, _, callErr = procCreateProcessW.Call(
		0, uintptr(unsafe.Pointer(cmdline)), 0, 0, 0,
		extendedStartupInfoPresent|createUnicodeEnvironment,
		envBlock(), uintptr(unsafe.Pointer(cwdPtr)),
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if hr == 0 {
		procDeleteProcThreadAttrList.Call(uintptr(unsafe.Pointer(&attr[0])))
		return nil, fmt.Errorf("CreateProcessW: %v", callErr)
	}
	syscall.CloseHandle(pi.Thread)
	proc, err := os.FindProcess(int(pi.ProcessID))
	if err != nil {
		return nil, err
	}
	s.proc = proc
	return &attr[0], nil
}

// envBlock 构造 UTF-16 环境块（继承父进程 + 终端会话需要的 TERM）。
func envBlock() uintptr {
	lines := os.Environ()
	haveTerm := false
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), "TERM=") {
			haveTerm = true
			break
		}
	}
	if !haveTerm {
		lines = append(lines, "TERM=xterm-256color")
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte(0)
	}
	b.WriteByte(0)
	u16 := utf16.Encode([]rune(b.String()))
	return uintptr(unsafe.Pointer(&u16[0]))
}

type conptySession struct {
	hpc    uintptr
	attr   *byte
	inW    *os.File
	outR   *os.File
	proc   *os.Process
	closeOnce sync.Once
}

func (s *conptySession) Read(p []byte) (int, error)  { return s.outR.Read(p) }
func (s *conptySession) Write(p []byte) (int, error) { return s.inW.Write(p) }

func (s *conptySession) Resize(cols, rows uint16) error {
	hr, _, callErr := procResizePseudoConsole.Call(s.hpc, uintptr(cols)|uintptr(rows)<<16)
	if hr != 0 {
		return fmt.Errorf("ResizePseudoConsole: %v", callErr)
	}
	return nil
}

func (s *conptySession) Wait() (int, error) {
	if s.proc == nil {
		return -1, fmt.Errorf("shell 进程未启动")
	}
	state, err := s.proc.Wait()
	if err != nil {
		return -1, err
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok {
		return int(ws.ExitCode), nil
	}
	return state.ExitCode(), nil
}

// Close 关闭伪控制台（conhost 断开后 shell 通常自行退出），仍活着就整树
// 强杀，最后释放 attribute list 与管道。
func (s *conptySession) Close() error {
	s.closeOnce.Do(func() {
		procClosePseudoConsole.Call(s.hpc)
		if s.proc != nil {
			_ = exec.Command("taskkill.exe", "/PID", fmt.Sprint(s.proc.Pid), "/T", "/F").Run()
		}
		if s.attr != nil {
			procDeleteProcThreadAttrList.Call(uintptr(unsafe.Pointer(s.attr)))
		}
		s.inW.Close()
		s.outR.Close()
	})
	return nil
}
