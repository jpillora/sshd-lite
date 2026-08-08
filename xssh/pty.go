package xssh

import (
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
)

// PTY is an interface that abstracts platform-specific PTY implementations.
// It provides read/write capabilities and a file descriptor holder for resizing.
type PTY interface {
	io.ReadWriteCloser
	FdHolder
}

// FdHolder is an interface for types that can return their file descriptor.
type FdHolder interface {
	Fd() uintptr
}

// Winsize describes the terminal size.
type Winsize struct {
	Rows uint16
	Cols uint16
}

// startPTY starts a command with a PTY attached.
// If ws is non-nil, the PTY is opened at that size atomically.
// Platform-specific implementations are in pty_unix.go and pty_win.go.
var startPTY func(cmd *exec.Cmd, ws *Winsize) (PTY, error)

// setWinsize applies an already validated terminal size. Platform-specific
// implementations are installed alongside startPTY.
var setWinsize func(t FdHolder, ws *Winsize) error

// Windows' COORD type uses signed int16 fields. Keep one predictable limit on
// every platform so a size accepted on Unix cannot wrap when used on Windows.
const maxTerminalDimension = uint32(1<<15 - 1)

// winsizeFromDimensions validates SSH's uint32 character dimensions before
// narrowing them to the uint16 fields supported by our PTY implementations.
// RFC 4254 requires zero dimensions to be ignored. Since a resize needs both
// character dimensions, either zero makes the entire size update a no-op.
func winsizeFromDimensions(cols, rows uint32) (*Winsize, error) {
	if cols > maxTerminalDimension || rows > maxTerminalDimension {
		return nil, fmt.Errorf("terminal dimensions out of range: %dx%d (maximum %dx%d)", cols, rows, maxTerminalDimension, maxTerminalDimension)
	}
	if cols == 0 || rows == 0 {
		return nil, nil
	}
	return &Winsize{Cols: uint16(cols), Rows: uint16(rows)}, nil
}

// parseDims parses the canonical character-dimension payload used by
// Session.Resizes. Requiring the exact size keeps malformed public-channel
// writes from reaching the platform resize implementation.
func parseDims(b []byte) (*Winsize, error) {
	if len(b) != 8 {
		return nil, fmt.Errorf("malformed terminal dimensions: got %d bytes, want 8", len(b))
	}
	return winsizeFromDimensions(binary.BigEndian.Uint32(b[:4]), binary.BigEndian.Uint32(b[4:]))
}

func marshalDims(ws *Winsize) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[:4], uint32(ws.Cols))
	binary.BigEndian.PutUint32(b[4:], uint32(ws.Rows))
	return b
}

// SetWinsize validates and sets the size of the given PTY. Zero dimensions are
// ignored as required by RFC 4254; dimensions beyond the cross-platform PTY
// limit are rejected. Running PTY resize is unavailable on Windows because the
// current backend cannot synchronize it with its asynchronous ConPTY close.
func SetWinsize(t FdHolder, cols, rows uint32) error {
	ws, err := winsizeFromDimensions(cols, rows)
	if err != nil || ws == nil {
		return err
	}
	return setWinsize(t, ws)
}
