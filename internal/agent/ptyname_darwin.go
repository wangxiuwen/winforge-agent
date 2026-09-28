//go:build darwin

package agent

import (
	"bytes"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ptySlavePath 返回 master 对应的 slave 设备路径。darwin 打开 /dev/ptmx 后
// 要先 TIOCPTYGRANT 把 slave 授权给当前 uid（否则 open 报 permission
// denied），再 TIOCPTYUNLK 解锁（否则 open 报 unavailable），最后
// TIOCPTYGNAME 取名字。
func ptySlavePath(fd int) (string, error) {
	for _, req := range []struct {
		name uintptr
		arg  uintptr
	}{
		{unix.TIOCPTYGRANT, 0},
		{unix.TIOCPTYUNLK, 0},
	} {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req.name, req.arg); errno != 0 {
			return "", fmt.Errorf("ioctl 0x%x: %v", req.name, errno)
		}
	}
	buf := make([]byte, 128)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return "", fmt.Errorf("TIOCPTYGNAME: %v", errno)
	}
	return string(buf[:bytes.IndexByte(buf, 0)]), nil
}
