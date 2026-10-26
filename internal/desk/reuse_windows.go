//go:build windows

package desk

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// reuseControl 在 Windows 上只设置 SO_REUSEADDR；Windows 没有 SO_REUSEPORT。
// 多个 desk 实例（或与系统服务）共用发现端口时依赖它。
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
