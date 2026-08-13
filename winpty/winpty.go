// Package winpty provides a Windows ConPTY implementation behind a small,
// creack/pty-shaped API (Start, StartWithSize, Setsize).
//
// The Windows implementation is vendored from github.com/photostorm/pty, a fork
// of github.com/creack/pty that added ConPTY support. It is vendored rather than
// imported because that fork's go.mod still declares its module path as
// github.com/creack/pty, so depending on it requires a `replace` directive — and
// replace directives in a non-main module are ignored by every consumer, which
// made this package impossible to import or `go install`.
//
// See LICENSE-creack-pty for the upstream copyright and license.
package winpty

import (
	"errors"
	"io"
	"os/exec"
)

// ErrUnsupported is returned by every entry point on non-Windows platforms.
var ErrUnsupported = errors.New("winpty: ConPTY is only available on windows")

// FdHolder is an interface for types that can return their file descriptor.
type FdHolder interface {
	Fd() uintptr
}

// Winsize describes the terminal size.
type Winsize struct {
	Rows uint16 // ws_row: Number of rows (in cells)
	Cols uint16 // ws_col: Number of columns (in cells)
	X    uint16 // ws_xpixel: Width in pixels
	Y    uint16 // ws_ypixel: Height in pixels
}

// Pty is the controlling side of a pseudo-terminal. On Windows the concrete
// type is *WindowsPty, wrapping a ConPTY handle and its two pipe ends.
type Pty interface {
	// Fd returns the handle used to resize the child's terminal.
	FdHolder

	Name() string

	// WriteString is only used to identify Pty and Tty.
	WriteString(s string) (n int, err error)
	io.ReadWriteCloser
}

// Tty is the child side of a pseudo-terminal. On Windows the concrete type is
// *WindowsTty, a combination of two pipe files.
type Tty interface {
	// Fd is only intended for manually inheriting the size from a Pty.
	FdHolder

	Name() string

	io.ReadWriteCloser
}

// Start starts a new process connected to a pty and returns the pty handle.
func Start(cmd *exec.Cmd) (Pty, error) {
	return StartWithSize(cmd, nil)
}
