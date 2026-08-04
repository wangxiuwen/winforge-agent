//go:build !windows

package main

import "fmt"

func maybeRunAsService() (bool, error) { return false, nil }

func runService(_ []string) error {
	return fmt.Errorf("service 命令只支持 Windows")
}
