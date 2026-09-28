//go:build linux

package agent

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ptySlavePath 先解锁从端（TIOCSPTLCK=0），再按 TIOCGPTN 拼出 /dev/pts/N。
func ptySlavePath(fd int) (string, error) {
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		return "", fmt.Errorf("解锁从端: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		return "", fmt.Errorf("TIOCGPTN: %w", err)
	}
	return fmt.Sprintf("/dev/pts/%d", n), nil
}
