//go:build !windows
// +build !windows

package sshd

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func init() {
	startPTY = func(cmd *exec.Cmd) (PTY, error) {
		f, err := pty.Start(cmd)
		if err != nil {
			return nil, err
		}
		if _, err := term.MakeRaw(int(f.Fd())); err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t FdHolder, w, h uint32) {
	ws := &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	// Type assert to *os.File since that's what creack/pty expects
	if f, ok := t.(*os.File); ok {
		// PTY resize errors are non-fatal - terminal continues with previous size
		resizeErr := pty.Setsize(f, ws)
		if resizeErr != nil {
			// Error is intentionally ignored - PTY resize failures are non-fatal
		}
	}
}
