package sshd

import (
	"encoding/binary"

	"github.com/creack/pty"
)

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// SetWinsize sets the size of the given pty.
func SetWinsize(t pty.FdHolder, w, h uint32) {
	ws := &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}
	pty.Setsize(t, ws)
}

// Borrowed from https://github.com/creack/termios/blob/master/win/win.go
