//go:build !windows

package agent

import "golang.org/x/text/encoding"

// consoleOutputDecoder 非 Windows 平台子进程输出即 UTF-8，无需转码。
func consoleOutputDecoder() *encoding.Decoder { return nil }
