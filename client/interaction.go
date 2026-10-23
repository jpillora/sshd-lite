package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// HostKey identifies an untrusted ordinary host key, never a replacement key or CA.
type HostKey struct{ Host, Algorithm, Fingerprint string }

// Interaction callbacks must honor ctx. Nil callbacks are noninteractive.
// Passphrase returns caller-owned bytes; Dial clears them after use. Callbacks
// are synchronous and serial within a connection, and may run concurrently for
// independent connections. They must not log secrets.
type Interaction struct {
	ConfirmHost func(context.Context, HostKey) (bool, error)
	Passphrase  func(context.Context, string) ([]byte, error)
}

var ErrInteractionRequired = errors.New("ssh client: interactive confirmation is required")
var ErrHostKeyRejected = errors.New("ssh client: host key rejected")

type interactionOwner struct{ conn net.Conn }

func (i *interactionOwner) run(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if i != nil && i.conn != nil {
		if err := i.conn.SetDeadline(time.Time{}); err != nil {
			return err
		}
		defer i.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	err := fn()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// TerminalInteraction prompts through the controlling terminal, never command
// stdin/stdout. No terminal is opened until a callback is actually needed.
func TerminalInteraction() Interaction {
	return Interaction{
		ConfirmHost: func(ctx context.Context, key HostKey) (bool, error) {
			prompt := fmt.Sprintf("The authenticity of host %s cannot be established.\r\n%s key fingerprint is %s.\r\nAdd this host to known_hosts? (yes/no) ", strconv.Quote(key.Host), key.Algorithm, key.Fingerprint)
			b, e := terminalRead(ctx, prompt, false)
			if e != nil {
				return false, e
			}
			defer clear(b)
			return string(b) == "yes" || string(b) == "y", nil
		},
		Passphrase: func(ctx context.Context, name string) ([]byte, error) {
			return terminalRead(ctx, "Passphrase for "+strconv.Quote(name)+": ", true)
		},
	}
}
