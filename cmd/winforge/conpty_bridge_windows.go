//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wangxiuwen/winforge-agent/internal/conpty"
)

// runConptyBridge 由 agent 经 exec 拉起（本进程 std 是管道——ConPTY 只在
// 管道 std 的调用者下工作，见 internal/conpty 的说明）。
//
// stdio 协议：
//   stdin  → 帧：0x01 [u16 len] [bytes] 键入；0x02 [u16 cols][u16 rows] resize
//   stdout ← ConPTY 原始字节流
//   stderr ← 控制事件行：{"event":"started"} / {"event":"exit","code":N} / {"event":"error","msg":...}
func runConptyBridge(args []string) error {
	fs := flag.NewFlagSet("conpty-bridge", flag.ContinueOnError)
	cols := fs.Uint("cols", 120, "列数")
	rows := fs.Uint("rows", 30, "行数")
	cwd := fs.String("cwd", "", "工作目录")
	shell := fs.String("shell", `powershell.exe -NoLogo`, "完整命令行")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := conpty.Start(*shell,
		conpty.ConPtyDimensions(int(*cols), int(*rows)),
		conpty.ConPtyWorkDir(*cwd),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"event":"error","msg":%q}`+"\n", strings.TrimSpace(fmt.Sprint(err)))
		return err
	}
	defer p.Close()
	fmt.Fprintln(os.Stderr, `{"event":"started"}`)

	// stderr 行作为控制通道：会话退出码从这里回报。
	eventDone := make(chan struct{})
	go func() {
		defer close(eventDone)
		code, _ := p.Wait(context.Background())
		fmt.Fprintf(os.Stderr, `{"event":"exit","code":%d}`+"\n", code)
	}()

	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			kind, err := reader.ReadByte()
			if err != nil {
				return // agent 端关 stdin：会话收尾
			}
			switch kind {
			case 0x01:
				var n uint16
				if err := binary.Read(reader, binary.LittleEndian, &n); err != nil {
					return
				}
				buf := make([]byte, n)
				if _, err := io.ReadFull(reader, buf); err != nil {
					return
				}
				if _, err := p.Write(buf); err != nil {
					return
				}
			case 0x02:
				var size struct{ C, R uint16 }
				if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
					return
				}
				_ = p.Resize(int(size.C), int(size.R))
			}
		}
	}()

	buf := make([]byte, 32<<10)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			if _, werr := os.Stdout.Write(buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	<-eventDone
	return nil
}
