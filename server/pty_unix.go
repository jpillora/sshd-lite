//go:build !windows
// +build !windows

package sshd

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func init() {
	startPTY = func(cmd *exec.Cmd) (PTY, error) {
		return pty.Start(cmd)
	}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) {
	ws := &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	// Type assert to *os.File since that's what creack/pty expects
	if f, ok := t.(*os.File); ok {
		pty.Setsize(f, ws)
	}
}
