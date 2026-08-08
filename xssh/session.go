package xssh

import (
	"log/slog"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Session represents an active SSH session with its associated state.
// A session is created when a "session" channel is accepted.
type Session struct {
	conn Conn
	done chan struct{}
	stop sync.Once
	// Channel is the underlying SSH "session" channel.
	Channel ssh.Channel
	// Env contains environment variables for this session.
	Env []string
	// Resizes receives terminal resize events (window-change requests).
	// Each payload contains exactly width and height as uint32 big-endian
	// values. Built-in handlers coalesce pending events so this channel never
	// blocks the session request dispatcher.
	Resizes chan []byte
	// Logger for session-specific logging. If nil, uses connection logger.
	Logger *slog.Logger
}

var sessionNeverDone = make(chan struct{})

// Done is closed when the SSH session's request stream is torn down. This is
// distinct from EOF on Channel: a client may close its write side while a
// command continues producing output.
func (s *Session) Done() <-chan struct{} {
	if s.done == nil {
		// Preserve the behavior of Sessions constructed by users before Done was
		// added. Sessions accepted by xssh always have a real lifecycle signal.
		return sessionNeverDone
	}
	return s.done
}

func (s *Session) closeDone() {
	if s.done != nil {
		s.stop.Do(func() { close(s.done) })
	}
}

// Conn returns the connection this session belongs to.
func (s *Session) Conn() Conn {
	return s.conn
}

// Config returns the connection configuration.
func (s *Session) Config() *Config {
	return s.conn.Config()
}
