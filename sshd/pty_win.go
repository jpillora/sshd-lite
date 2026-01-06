//go:build windows
// +build windows

package sshd

import (
	"os/exec"

	"github.com/jpillora/sshd-lite/winpty"
)

func init() {
	startPTY = func(cmd *exec.Cmd) (PTY, error) {
		// winpty.Start returns pty.Pty which implements PTY interface
		return winpty.Start(cmd)
	}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) {
	ws := &winpty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	winpty.Setsize(t, ws)
}
