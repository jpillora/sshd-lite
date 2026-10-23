package xssh

import (
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Session represents an active SSH session with its associated state.
// A session is created when a "session" channel is accepted.
type Session struct {
	conn  Conn
	done  chan struct{}
	stop  sync.Once
	tasks sync.WaitGroup
	pty   bool
	// phase is owned by the serial session request dispatcher, never workers.
	phase sessionPhase
	// Channel is the underlying SSH "session" channel.
	Channel ssh.Channel
	// Env contains environment variables for this session. Built-in env/PTY
	// setup is accepted only before program admission. Custom handlers must
	// not mutate launch settings after starting work that reads them.
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
	s.phase = sessionClosing
	if s.done != nil {
		s.stop.Do(func() { close(s.done) })
	}
}

type sessionPhase uint8

const (
	sessionConfiguring sessionPhase = iota
	sessionStarting
	sessionRunning
	sessionClosing
)

// dispatchRequest reserves program ownership before invoking a handler, including
// custom shell/exec/subsystem handlers. A rejection leaves configuration open;
// once success is reported, even a later process-start error consumes the slot.
func (s *Session) dispatchRequest(req *Request, handler func() error) error {
	if s.phase == sessionClosing {
		return fmt.Errorf("session is closing")
	}
	program := req.Type == ShellRequestType || req.Type == ExecRequestType || req.Type == "subsystem"
	setup := req.Type == EnvRequestType || req.Type == PTYRequestType
	if (program || setup) && s.phase != sessionConfiguring {
		return fmt.Errorf("session program already admitted")
	}
	if program {
		s.phase = sessionStarting
	}
	err := handler()
	if program {
		accepted := err == nil
		if req.Replied() {
			accepted = req.accepted
		}
		if accepted {
			s.phase = sessionRunning
		} else {
			s.phase = sessionConfiguring
		}
	}
	return err
}

func (s *Session) startTasks() {
	s.tasks.Add(1)
}

func (s *Session) goTask(fn func()) {
	s.tasks.Add(1)
	go func() {
		defer s.tasks.Done()
		fn()
	}()
}

func (s *Session) waitTasks() {
	s.tasks.Done()
	s.tasks.Wait()
}

// Conn returns the connection this session belongs to.
func (s *Session) Conn() Conn {
	return s.conn
}

// Config returns the connection configuration.
func (s *Session) Config() *Config {
	return s.conn.Config()
}
