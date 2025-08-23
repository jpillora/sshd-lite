package client

import (
	"io"
	"os"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// TerminalSession represents a terminal session that can be either SSH-based or PTY-based
type TerminalSession interface {
	// IO operations
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	
	// Terminal control
	RequestPty(term string, h, w int, modes ssh.TerminalModes) error
	WindowChange(h, w int) error
	Shell() error
	Close() error
	
	// Stream assignment (for SSH compatibility)
	SetStdin(io.Reader)
	SetStdout(io.Writer) 
	SetStderr(io.Writer)
}

// SSHSession wraps an SSH session to implement TerminalSession
type SSHSession struct {
	session   *ssh.Session
	stdinPipe io.WriteCloser
}

func NewSSHSession(session *ssh.Session) *SSHSession {
	return &SSHSession{session: session}
}

func (s *SSHSession) Read(p []byte) (int, error) {
	// SSH sessions don't support direct reading like this
	// Reading is handled through the Stdout/Stderr pipes
	return 0, io.ErrNoProgress
}

func (s *SSHSession) Write(p []byte) (int, error) {
	if s.stdinPipe == nil {
		pipe, err := s.session.StdinPipe()
		if err != nil {
			return 0, err
		}
		s.stdinPipe = pipe
	}
	return s.stdinPipe.Write(p)
}

func (s *SSHSession) RequestPty(term string, h, w int, modes ssh.TerminalModes) error {
	return s.session.RequestPty(term, h, w, modes)
}

func (s *SSHSession) WindowChange(h, w int) error {
	return s.session.WindowChange(h, w)
}

func (s *SSHSession) Shell() error {
	return s.session.Shell()
}

func (s *SSHSession) Close() error {
	if s.stdinPipe != nil {
		s.stdinPipe.Close()
	}
	return s.session.Close()
}

func (s *SSHSession) SetStdin(r io.Reader) {
	s.session.Stdin = r
}

func (s *SSHSession) SetStdout(w io.Writer) {
	s.session.Stdout = w
}

func (s *SSHSession) SetStderr(w io.Writer) {
	s.session.Stderr = w
}

// PTYSession wraps a PTY to implement TerminalSession
type PTYSession struct {
	pty    pty.Pty
	tty    pty.Tty
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func NewPTYSession(ptyFile pty.Pty) *PTYSession {
	return &PTYSession{
		pty: ptyFile,
	}
}

func (p *PTYSession) Read(data []byte) (int, error) {
	if p.pty == nil {
		return 0, io.ErrClosedPipe
	}
	return p.pty.Read(data)
}

func (p *PTYSession) Write(data []byte) (int, error) {
	if p.pty == nil {
		return 0, io.ErrClosedPipe
	}
	return p.pty.Write(data)
}

func (p *PTYSession) RequestPty(term string, h, w int, modes ssh.TerminalModes) error {
	// For PTY sessions, this is essentially a resize operation
	return p.WindowChange(h, w)
}

func (p *PTYSession) WindowChange(h, w int) error {
	if p.pty == nil {
		return os.ErrInvalid
	}
	return pty.Setsize(p.pty, &pty.Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	})
}

func (p *PTYSession) Shell() error {
	// For PTY sessions, shell is already running
	return nil
}

func (p *PTYSession) Close() error {
	if p.pty != nil {
		return p.pty.Close()
	}
	return nil
}

func (p *PTYSession) SetStdin(r io.Reader) {
	p.stdin = r
}

func (p *PTYSession) SetStdout(w io.Writer) {
	p.stdout = w
}

func (p *PTYSession) SetStderr(w io.Writer) {
	p.stderr = w
}

// WebSocketSession wraps a WebSocket connection to work with PTY sessions
type WebSocketSession struct {
	ptySession *PTYSession
	wsReader   io.Reader
	wsWriter   io.Writer
}

func NewWebSocketSession(ptySession *PTYSession, wsReader io.Reader, wsWriter io.Writer) *WebSocketSession {
	return &WebSocketSession{
		ptySession: ptySession,
		wsReader:   wsReader,
		wsWriter:   wsWriter,
	}
}

func (w *WebSocketSession) Read(p []byte) (int, error) {
	return w.ptySession.Read(p)
}

func (w *WebSocketSession) Write(p []byte) (int, error) {
	return w.ptySession.Write(p)
}

func (w *WebSocketSession) RequestPty(term string, h, cols int, modes ssh.TerminalModes) error {
	return w.ptySession.RequestPty(term, h, cols, modes)
}

func (w *WebSocketSession) WindowChange(h, cols int) error {
	return w.ptySession.WindowChange(h, cols)
}

func (w *WebSocketSession) Shell() error {
	return w.ptySession.Shell()
}

func (w *WebSocketSession) Close() error {
	return w.ptySession.Close()
}

func (w *WebSocketSession) SetStdin(r io.Reader) {
	w.ptySession.SetStdin(r)
}

func (w *WebSocketSession) SetStdout(writer io.Writer) {
	w.ptySession.SetStdout(writer)
}

func (w *WebSocketSession) SetStderr(writer io.Writer) {
	w.ptySession.SetStderr(writer)
}

// StartWebSocketProxy starts copying data between WebSocket and PTY
func (w *WebSocketSession) StartWebSocketProxy() {
	// Copy from PTY to WebSocket
	go func() {
		io.Copy(w.wsWriter, w.ptySession.pty)
	}()
	
	// Copy from WebSocket to PTY  
	go func() {
		io.Copy(w.ptySession.pty, w.wsReader)
	}()
}