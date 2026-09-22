//go:build !windows

package main

// runSetup 只在 Windows 上有意义：双击/右键运行时一步装好服务。
func runSetup() (bool, error) { return false, nil }
