package client

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func ReplaceTerminal(session *ssh.Session) error {
	termSession := NewSSHSession(session)
	return ReplaceTerminalWithSession(termSession)
}

func ReplaceTerminalWithSession(session TerminalSession) error {
	stdin := os.Stdin
	stdout := os.Stdout
	stderr := os.Stderr

	if term.IsTerminal(int(stdin.Fd())) {
		state, err := term.MakeRaw(int(stdin.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(stdin.Fd()), state)

		w, h, err := term.GetSize(int(stdin.Fd()))
		if err != nil {
			return err
		}

		if err := session.RequestPty("xterm", h, w, ssh.TerminalModes{}); err != nil {
			return err
		}

		session.SetStdin(stdin)
		session.SetStdout(stdout)
		session.SetStderr(stderr)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGWINCH)
		go func() {
			for range sigCh {
				w, h, err := term.GetSize(int(stdin.Fd()))
				if err == nil {
					session.WindowChange(h, w)
				}
			}
		}()
	} else {
		session.SetStdin(stdin)
		session.SetStdout(stdout)
		session.SetStderr(stderr)
	}

	return session.Shell()
}

// AttachWebSocketToSession connects a WebSocket to a PTY session
func AttachWebSocketToSession(ptySession *PTYSession, wsReader io.Reader, wsWriter io.Writer) *WebSocketSession {
	wsSession := NewWebSocketSession(ptySession, wsReader, wsWriter)
	wsSession.StartWebSocketProxy()
	return wsSession
}