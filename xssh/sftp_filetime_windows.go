//go:build windows

package xssh

import (
	"os"
	"time"
)

func setFileTimes(root *os.Root, _ *os.File, name string, atime, mtime time.Time) error {
	// os.Root's path-based metadata operations do not have the Unix symlink
	// race on Windows and open files do not necessarily grant write-attributes.
	return root.Chtimes(name, atime, mtime)
}
