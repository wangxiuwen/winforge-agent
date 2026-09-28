//go:build windows

package agent

import (
	"golang.org/x/text/encoding"
	"syscall"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procGetOEMCP           = kernel32.NewProc("GetOEMCP")
)

// consoleOutputDecoder 返回本机子进程管道输出的解码器。Agent 以服务方式
// 运行、没有控制台，GetConsoleOutputCP 会退回默认值，所以优先 GetOEMCP
// （cmd/PowerShell 管道输出使用的正是 OEM 代码页）。
func consoleOutputDecoder() *encoding.Decoder {
	enc := pickConsoleEncoding()
	if enc == nil {
		return nil
	}
	return enc.NewDecoder()
}

func pickConsoleEncoding() encoding.Encoding {
	if p := procGetConsoleOutputCP; p.Find() == nil {
		if cp, _, _ := p.Call(); cp != 0 && cp != 65001 {
			return decoderForCodePage(uint(cp))
		}
	}
	cp, _, _ := procGetOEMCP.Call()
	return decoderForCodePage(uint(cp))
}
