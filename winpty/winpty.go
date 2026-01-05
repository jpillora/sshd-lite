// Package winpty provides a wrapper around photostorm/pty for Windows support.
// This package exposes the same symbols as creack/pty but uses photostorm's fork
// which provides Windows PowerShell support.
package winpty

import (
	"os/exec"

	"github.com/creack/pty"
)

// FdHolder is an interface for types that can return their file descriptor.
type FdHolder = pty.FdHolder

// Winsize represents terminal window size.
type Winsize = pty.Winsize

// Start starts a new process connected to a pty and returns the pty handle.
// On Windows with photostorm/pty, this returns a pty.Pty interface which
// implements io.ReadWriteCloser and FdHolder.
func Start(cmd *exec.Cmd) (pty.Pty, error) {
	return pty.Start(cmd)
}

// Setsize sets the size of the given pty.
func Setsize(t FdHolder, ws *Winsize) error {
	return pty.Setsize(t, ws)
}
