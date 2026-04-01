package xssh

import (
	"encoding/binary"
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

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}
