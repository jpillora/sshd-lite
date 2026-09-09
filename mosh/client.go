package mosh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/jpillora/sshd-lite/internal/sshconn"

	protocol "github.com/jpillora/sshd-lite/internal/mosh"
	"golang.org/x/crypto/ssh"
)

// ClientConfig configures a programmatic terminal. It does not inspect the local
// terminal, load keys, prompt for credentials, or install signal handlers.
type ClientConfig struct {
	// Term defaults to xterm-256color. Columns and Rows default to 80 and 24.
	Term          string
	Columns, Rows int
	// Server is the remote executable, defaulting to mosh-server. Command is a
	// literal argv to run in its terminal; an empty slice starts the remote shell.
	Server  string
	Command []string
	// Output receives ANSI screen updates from one goroutine. Nil discards output.
	// Writes must return promptly; unblock a blocking writer before Close or Wait.
	// The caller owns Output and must not access it concurrently without locking.
	Output io.Writer
}

func (c ClientConfig) request() (protocol.Request, error) {
	if c.Columns == 0 {
		c.Columns = 80
	}
	if c.Rows == 0 {
		c.Rows = 24
	}
	if c.Term == "" {
		c.Term = "xterm-256color"
	}
	if c.Columns < 1 || c.Rows < 1 || c.Columns > 1000 || c.Rows > 1000 {
		return protocol.Request{}, fmt.Errorf("invalid terminal size %dx%d", c.Columns, c.Rows)
	}
	req := protocol.Request{Term: c.Term, Cols: uint16(c.Columns), Rows: uint16(c.Rows)}
	return req, req.Validate()
}

// Session is a running UDP terminal. Construct one with Dial or
// Start. Its methods may be called concurrently. It owns its network
// connection and worker goroutines; cancel its context or call Close when done.
//
// Unlike Run, this API does not change terminal modes or interpret Ctrl-^ . as
// an escape. Unmodified cursor keys should use application-mode (SS3) encoding,
// as standard Mosh requires. Output is an ANSI screen, not a byte-stream stdout.
type Session struct {
	ctx               context.Context
	cancel            context.CancelFunc
	input             chan []byte
	sizes             chan protocol.Request
	done              chan struct{}
	writeMu, resizeMu sync.Mutex
	code              int
	err               error
}

// Dial authenticates over SSH using only sshConfig, bootstraps a terminal,
// then closes SSH and returns a UDP session. address is an SSH host:port.
// sshConfig must specify authentication and a host-key callback. No credentials
// are read from files or an agent unless the supplied callbacks do so.
// SSH dialing defaults to ten seconds (overridden by sshConfig.Timeout); the
// handshake is bounded to thirty seconds and Mosh bootstrap to ten seconds.
func Dial(ctx context.Context, address string, sshConfig *ssh.ClientConfig, c ClientConfig) (*Session, error) {
	if _, err := c.request(); err != nil {
		return nil, err
	}
	conn, err := sshconn.Dial(ctx, address, sshConfig)
	if err != nil {
		return nil, err
	}
	return Start(ctx, conn, c)
}

// Start bootstraps Mosh over an already authenticated SSH connection.
// It takes ownership of conn and closes it before returning, on success or
// failure. Use a dedicated connection, with no other active SSH sessions.
// The supplied context controls the entire resulting Mosh session's lifetime.
func Start(ctx context.Context, conn *ssh.Client, c ClientConfig) (*Session, error) {
	if conn == nil {
		return nil, fmt.Errorf("SSH connection is required")
	}
	defer conn.Close()
	req, err := c.request()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credentials, err := bootstrapMosh(ctx, conn, req, c.Server, c.Command)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	// Use the authenticated peer, avoiding different DNS answers for SSH and UDP.
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil, err
	}
	conn.Close()
	udp, err := (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(credentials.Port)))
	if err != nil {
		return nil, err
	}
	output := c.Output
	if output == nil {
		output = io.Discard
	}
	runCtx, cancel := context.WithCancel(ctx)
	session := &Session{ctx: runCtx, cancel: cancel, input: make(chan []byte, 16), sizes: make(chan protocol.Request, 1), done: make(chan struct{})}
	session.sizes <- req
	go func() {
		session.code, session.err = protocol.RunClient(runCtx, udp, credentials.Key, req, session.input, session.sizes, output)
		// Cancellation wins over a racing remote shutdown acknowledgement.
		if session.err == nil && runCtx.Err() != nil {
			session.code, session.err = 0, runCtx.Err()
		}
		cancel()
		close(session.done)
	}()
	return session, nil
}

// Write copies and queues terminal input, applying bounded backpressure during
// outages. A successful write means queued, not acknowledged by the server.
// A zero-length write does not send EOF; Mosh has no stdin EOF operation.
func (s *Session) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n := 0
	for len(p) > 0 {
		if err := s.writeError(); err != nil {
			return n, err
		}
		size := min(len(p), 4096)
		b := append([]byte(nil), p[:size]...)
		select {
		case s.input <- b:
			n += size
			p = p[size:]
		case <-s.ctx.Done():
			return n, s.writeError()
		}
	}
	return n, nil
}
func (s *Session) writeError() error {
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	return s.ctx.Err()
}

// Resize queues the latest terminal size; intermediate pending resizes may be
// coalesced. It does not wait for the remote PTY to apply the change.
func (s *Session) Resize(columns, rows int) error {
	if columns < 1 || rows < 1 {
		return fmt.Errorf("invalid terminal size %dx%d", columns, rows)
	}
	req, err := (ClientConfig{Columns: columns, Rows: rows}).request()
	if err != nil {
		return err
	}
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	if err := s.writeError(); err != nil {
		return err
	}
	select {
	case <-s.sizes:
	default:
	}
	select {
	case s.sizes <- req:
		return nil
	case <-s.ctx.Done():
		return s.writeError()
	}
}

// Done closes after the network workers stop and no further Output writes occur.
func (s *Session) Done() <-chan struct{} { return s.done }

// Wait waits for completion and returns the remote exit status when supported.
// Standard Mosh has no exit status; a clean standard-peer shutdown returns zero.
// Local cancellation returns the context error. Wait can be called repeatedly.
func (s *Session) Wait() (int, error) { <-s.done; return s.code, s.err }

// Close requests graceful shutdown and waits for completion. It is idempotent.
// The protocol allows up to three seconds to acknowledge local shutdown.
// Close does not close Output. Use Wait to inspect the session result.
func (s *Session) Close() error { s.cancel(); <-s.done; return nil }
