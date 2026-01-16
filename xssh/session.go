package xssh

import (
	"log/slog"

	"golang.org/x/crypto/ssh"
)

// Session represents an active SSH session with its associated state.
// A session is created when a "session" channel is accepted.
type Session struct {
	conn Conn
	// Channel is the underlying SSH "session" channel.
	Channel ssh.Channel
	// Env contains environment variables for this session.
	Env []string
	// Resizes receives terminal resize events (window-change requests).
	// Each payload contains width and height as uint32 big-endian values.
	Resizes chan []byte
	// Logger for session-specific logging. If nil, uses connection logger.
	Logger *slog.Logger
}

// Conn returns the connection this session belongs to.
func (s *Session) Conn() Conn {
	return s.conn
}

// Config returns the connection configuration.
func (s *Session) Config() *Config {
	return s.conn.Config()
}
