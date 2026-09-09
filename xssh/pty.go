package xssh

import (
	"encoding/binary"
	"fmt"

	"github.com/jpillora/sshd-lite/internal/terminal"
)

// PTY is an interface that abstracts platform-specific PTY implementations.
// It provides read/write capabilities and a file descriptor holder for resizing.
type PTY = terminal.PTY

// FdHolder is implemented by file-descriptor-backed terminals.
type FdHolder = terminal.FdHolder

// Winsize describes character dimensions.
type Winsize = terminal.Size

// Windows' COORD type uses signed int16 fields. Keep one predictable limit on
// every platform so a size accepted on Unix cannot wrap when used on Windows.
const maxTerminalDimension = terminal.MaxDimension

// winsizeFromDimensions validates SSH's uint32 character dimensions before
// narrowing them to the uint16 fields supported by our PTY implementations.
// RFC 4254 requires zero dimensions to be ignored. Since a resize needs both
// character dimensions, either zero makes the entire size update a no-op.
func winsizeFromDimensions(cols, rows uint32) (*Winsize, error) {
	return terminal.Dimensions(cols, rows)
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
	return terminal.SetSize(t, ws)
}
