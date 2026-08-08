//go:build !windows
// +build !windows

package xssh

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

const supportsRunningPTYResize = true

func init() {
	startPTY = func(cmd *exec.Cmd, ws *Winsize) (PTY, error) {
		var pws *pty.Winsize
		if ws != nil {
			pws = &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols}
		}
		return pty.StartWithSize(cmd, pws)
	}
	setWinsize = func(t FdHolder, ws *Winsize) error {
		f, ok := t.(*os.File)
		if !ok {
			return fmt.Errorf("SetWinsize: expected *os.File, got %T", t)
		}
		return pty.Setsize(f, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
	}
}

// closeShellPTY releases the shell PTY after the process has exited. On unix the
// PTY is owned by this package (creack/pty does not close it automatically), so
// we close it here to unblock the io.Copy goroutines.
func closeShellPTY(p PTY) {
	_ = p.Close()
}
