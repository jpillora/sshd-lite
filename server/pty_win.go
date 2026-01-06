//go:build windows
// +build windows

package sshd

import (
	"os/exec"

	"github.com/creack/pty"
)

type FdHolder = pty.FdHolder

type Winsize = pty.Winsize

func startPTY(cmd *exec.Cmd) (PTY, error) {
	return pty.Start(cmd)
}

func SetWinsize(t FdHolder, w, h uint32) {
	ws := &Winsize{Rows: uint16(h), Cols: uint16(w)}
	pty.Setsize(t, ws)
}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) {
	ws := &winpty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	winpty.Setsize(t, ws)
}
