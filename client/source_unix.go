//go:build unix

package client

import (
	"golang.org/x/sys/unix"
	"os"
)

// Nonblocking open lets the shared reader reject FIFOs/devices before reading.
func openSource(name string) (*os.File, error) {
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
