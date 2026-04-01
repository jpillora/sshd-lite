//go:build windows
// +build windows

package xssh

import (
	"os/exec"

	"github.com/jpillora/sshd-lite/winpty"
)

func init() {
	startPTY = func(cmd *exec.Cmd, ws *Winsize) (PTY, error) {
		p, err := winpty.Start(cmd)
		if err != nil {
			return nil, err
		}
		if ws != nil {
			_ = winpty.Setsize(p, &winpty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
		}
		return p, nil
	}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) error {
	ws := &winpty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	return winpty.Setsize(t, ws)
}
