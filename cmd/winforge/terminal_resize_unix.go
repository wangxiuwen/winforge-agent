//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/wangxiuwen/winforge-agent/internal/agent"
	"golang.org/x/term"
)

// watchTerminalResize 把 SIGWINCH 转成尺寸变化事件，随窗口拖动持续告诉
// 远端伪控制台。返回停止函数。
func watchTerminalResize(fd int, out chan<- agent.TermSize) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range sigs {
			if w, h, err := term.GetSize(fd); err == nil {
				select {
				case out <- agent.TermSize{Cols: uint16(w), Rows: uint16(h)}:
				default: // 尺寸挤着了就丢旧保新
				}
			}
		}
	}()
	return func() { signal.Stop(sigs) }
}
