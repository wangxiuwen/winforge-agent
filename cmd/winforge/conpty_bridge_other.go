//go:build !windows

package main

import "fmt"

// runConptyBridge 仅在 Windows 上有意义（ConPTY 是 Windows 的伪控制台）。
func runConptyBridge(args []string) error {
	return fmt.Errorf("conpty-bridge 仅在 Windows agent 上可用")
}
