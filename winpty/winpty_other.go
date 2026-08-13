//go:build !windows
// +build !windows

package winpty

import "os/exec"

// StartWithSize is unavailable off Windows. Callers on Unix use creack/pty
// directly; this package exists only to hold the ConPTY backend.
func StartWithSize(cmd *exec.Cmd, ws *Winsize) (Pty, error) {
	return nil, ErrUnsupported
}

// Setsize is unavailable off Windows.
func Setsize(t FdHolder, ws *Winsize) error {
	return ErrUnsupported
}

// GetsizeFull is unavailable off Windows.
func GetsizeFull(t FdHolder) (*Winsize, error) {
	return nil, ErrUnsupported
}
