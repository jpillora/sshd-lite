//go:build !windows
// +build !windows

package xssh

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func init() {
	startPTY = func(cmd *exec.Cmd, ws *Winsize) (PTY, error) {
		var pws *pty.Winsize
		if ws != nil {
			pws = &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols}
		}
		return pty.StartWithSize(cmd, pws)
	}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) error {
	f, ok := t.(*os.File)
	if !ok {
		return fmt.Errorf("SetWinsize: expected *os.File, got %T", t)
	}
	ws := &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	return pty.Setsize(f, ws)
}
