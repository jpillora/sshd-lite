// Package mosh embeds mosh-go's wire protocol on a shared UDP port.
package mosh

import (
	"fmt"
	"io"
	"time"
)

const (
	RequestName  = "mosh@sshd-lite"
	IdleTimeout  = 5 * time.Minute
	maxSessions  = 64
	tickInterval = 10 * time.Millisecond
	// Private session controls, carried in mosh-go's extensible control field.
	controlExit  uint32 = 0x53534801
	controlClose uint32 = 0x53534802
)

type Request struct {
	Term       string
	Cols, Rows uint16
}
type Credentials struct {
	Port int
	Key  string
}

func (r Request) Validate() error {
	// Bound the emulator's allocation as well as the platform's PTY size.
	if r.Cols == 0 || r.Rows == 0 || r.Cols > 1000 || r.Rows > 1000 || int(r.Cols)*int(r.Rows) > 100000 {
		return fmt.Errorf("invalid terminal size %dx%d", r.Cols, r.Rows)
	}
	if len(r.Term) > 128 {
		return fmt.Errorf("terminal name too long")
	}
	for _, c := range r.Term {
		if c < 32 || c > 126 {
			return fmt.Errorf("invalid terminal name")
		}
	}
	return nil
}

type Terminal interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	Wait() int
}
type StartTerminal func() (Terminal, error)
