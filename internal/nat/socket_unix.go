//go:build linux || darwin

package nat

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setSocketOptions sets SO_REUSEADDR and SO_REUSEPORT for hole-punching
func setSocketOptions(network, address string, c syscall.RawConn) error {
	var sockErr error
	err := c.Control(func(fd uintptr) {
		// SO_REUSEADDR allows multiple binds to same address
		sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if sockErr != nil {
			return
		}
		// SO_REUSEPORT allows multiple binds to same port
		sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})
	if err != nil {
		return err
	}
	return sockErr
}
