//go:build windows

package main

import "github.com/wangxiuwen/winforge-agent/internal/agent"

// watchTerminalResize Windows 客户端暂不监听窗口变化，连接时的尺寸已足够。
func watchTerminalResize(fd int, out chan<- agent.TermSize) func() { return func() {} }
