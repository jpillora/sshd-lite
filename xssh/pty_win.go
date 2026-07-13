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

// closeShellPTY releases the shell PTY after the process has exited. On Windows
// the ConPTY is owned and closed by photostorm/pty's internal waitProcess
// goroutine (run_windows.go). Closing it again here would call ClosePseudoConsole
// on an already-freed handle and corrupt the process heap (STATUS_HEAP_CORRUPTION),
// so this is intentionally a no-op — the library unblocks the io.Copy goroutines
// when it closes the ConPTY's pipe ends.
func closeShellPTY(p PTY) {
	// no-op: the pty library closes the ConPTY itself
}
