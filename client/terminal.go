package client

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func ReplaceTerminal(session *ssh.Session) error {
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

		session.Stdin = stdin
		session.Stdout = stdout
		session.Stderr = stderr

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
		session.Stdin = stdin
		session.Stdout = stdout
		session.Stderr = stderr
	}

	return session.Shell()
}