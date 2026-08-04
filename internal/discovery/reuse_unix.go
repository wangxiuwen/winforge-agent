//go:build !windows

package discovery

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reuseControl 允许与系统已有的 mDNS 响应器共用 5353 端口。
func reuseControl(network, address string, conn syscall.RawConn) error {
	var setErr error
	err := conn.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			setErr = err
			return
		}
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})
	if err != nil {
		return err
	}
	return setErr
}
