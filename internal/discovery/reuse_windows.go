//go:build windows

package discovery

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// reuseControl 在 Windows 上只设置 SO_REUSEADDR；Windows 没有 SO_REUSEPORT。
func reuseControl(network, address string, conn syscall.RawConn) error {
	var setErr error
	err := conn.Control(func(fd uintptr) {
		setErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET,
			windows.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return setErr
}
