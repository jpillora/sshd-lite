//go:build !unix

package client

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/jpillora/sshd-lite/internal/termio"
	"golang.org/x/term"
)

// Preserve Connect's existing blocking terminal behavior on non-Unix systems.
// This is deliberately separate from the cancellable Dial interaction contract.
func legacyTerminalRead(ctx context.Context, prompt string, secret bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tty, err := termio.OpenTTY()
	if err != nil {
		return nil, err
	}
	defer tty.Close()
	out, err := termio.OpenTTYOutput()
	if err != nil {
		return nil, err
	}
	defer out.Close()
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return nil, err
	}
	if secret {
		b, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(out)
		return b, err
	}
	line, err := bufio.NewReader(tty).ReadString('\n')
	return []byte(strings.TrimSpace(line)), err
}
