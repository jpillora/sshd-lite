//go:build windows
// +build windows

package terminal

import (
	"errors"
	"os/exec"

	"github.com/jpillora/sshd-lite/winpty"
)

// photostorm/pty owns and asynchronously closes the ConPTY handle when the
// child exits. It exposes no lock or closed signal that could make a concurrent
// ResizePseudoConsole safe. Initial sizing is still applied before child start.
const supportsRunningPTYResize = false

func init() {
	startPTY = func(cmd *exec.Cmd, ws *Size) (PTY, error) {
		var pws *winpty.Winsize
		if ws != nil {
			pws = &winpty.Winsize{Rows: ws.Rows, Cols: ws.Cols}
		}
		return winpty.StartWithSize(cmd, pws)
	}
	setSize = func(t FdHolder, ws *Size) error {
		return errors.New("SetWinsize: resizing a running ConPTY is unsafe with the current backend")
	}
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
