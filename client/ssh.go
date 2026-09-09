// Package client implements the sshd-lite SSH client.
package client

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/jpillora/sshd-lite/internal/termio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Run connects and runs a remote command or terminal, returning its exit status.
func Run(ctx context.Context, c Config, stdin *os.File, stdout, stderr io.Writer) (int, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	session, err := conn.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()
	session.Stdin, session.Stdout, session.Stderr = stdin, stdout, stderr
	if len(c.Command) > 0 {
		err = session.Run(strings.Join(c.Command, " "))
	} else {
		cols, rows := termio.Size(stdin)
		if term.IsTerminal(int(stdin.Fd())) {
			restore, err := termio.Raw(stdin)
			if err != nil {
				return 0, err
			}
			defer restore()
		}
		terminal := os.Getenv("TERM")
		if terminal == "" {
			terminal = "xterm-256color"
		}
		if err := session.RequestPty(terminal, rows, cols, ssh.TerminalModes{}); err != nil {
			return 0, err
		}
		cancelResize := termio.WatchResize(ctx, stdin, func(cols, rows int) { _ = session.WindowChange(rows, cols) })
		defer cancelResize()
		if err = session.Shell(); err == nil {
			err = session.Wait()
		}
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var status *ssh.ExitError
	if errors.As(err, &status) {
		return status.ExitStatus(), nil
	}
	return 0, err
}

// Connect authenticates using Config's identity, agent, known-hosts and password
// settings. The caller owns the returned connection. Use ssh.ClientConfig
// directly when your application supplies authentication without CLI defaults.
func Connect(ctx context.Context, c Config) (*ssh.Client, error) { return c.connect(ctx) }
