//go:build !windows

package desk

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reuseControl 允许多实例与系统服务共用发现端口。
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
