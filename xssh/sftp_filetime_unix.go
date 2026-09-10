//go:build linux || darwin || openbsd

package xssh

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func setFileTimes(_ *os.Root, file *os.File, _ string, atime, mtime time.Time) error {
	times := []unix.Timeval{
		unix.NsecToTimeval(atime.UnixNano()),
		unix.NsecToTimeval(mtime.UnixNano()),
	}
	return unix.Futimes(int(file.Fd()), times)
}
