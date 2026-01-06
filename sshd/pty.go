package sshd

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

// startPTY starts a command with a PTY attached.
// Platform-specific implementations are in pty_unix.go and pty_win.go.
var startPTY func(*exec.Cmd) (PTY, error)

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// Borrowed from https://github.com/creack/termios/blob/master/win/win.go
