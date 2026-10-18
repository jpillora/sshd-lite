package client

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
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
var ErrAuthentication = errors.New("ssh client: authentication failed")

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
func decryptIdentity(ctx context.Context, o DialOptions, name string, data []byte) (ssh.Signer, error) {
	if len(o.Passphrase) > 0 {
		s, e := ssh.ParsePrivateKeyWithPassphrase(data, o.Passphrase)
		if e == nil {
			return s, nil
		}
	}
	if o.resolved.batch || o.Interaction.Passphrase == nil {
		return nil, fmt.Errorf("%w: encrypted identity", ErrInteractionRequired)
	}
	for attempt := 0; attempt < 3; attempt++ {
		var phrase []byte
		err := o.interaction.run(ctx, func() error { var e error; phrase, e = o.Interaction.Passphrase(ctx, name); return e })
		if err != nil {
			clear(phrase)
			return nil, err
		}
		signer, e := ssh.ParsePrivateKeyWithPassphrase(data, phrase)
		clear(phrase)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e == nil {
			return signer, nil
		}
	}
	return nil, fmt.Errorf("%w: private key passphrase attempts exhausted", ErrAuthentication)
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
