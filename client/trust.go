package client

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
)

func prepareTrust(ctx context.Context, o DialOptions) (ssh.HostKeyCallback, error) {
	address := net.JoinHostPort(o.resolved.alias, o.Port)
	check, err := trustSnapshot(ctx, o.resolved.known, address)
	if err != nil {
		return nil, err
	}
	return func(_ string, remote net.Addr, key ssh.PublicKey) error {
		err := check(address, remote, key)
		if err == nil {
			return nil
		}
		var unknown *knownhosts.KeyError
		if !errors.As(err, &unknown) || len(unknown.Want) > 0 {
			return fmt.Errorf("%w: %w", ErrHostKeyRejected, err)
		}
		if _, certificate := key.(*ssh.Certificate); certificate {
			return fmt.Errorf("%w: host certificates require a pretrusted CA", ErrHostKeyRejected)
		}
		if o.resolved.strict == "yes" || o.resolved.batch || o.Interaction.ConfirmHost == nil {
			return fmt.Errorf("%w: unknown host %s (%s %s)", ErrInteractionRequired, address, key.Type(), ssh.FingerprintSHA256(key))
		}
		accepted := false
		if err := o.interaction.run(ctx, func() error {
			var e error
			accepted, e = o.Interaction.ConfirmHost(ctx, HostKey{address, key.Type(), ssh.FingerprintSHA256(key)})
			return e
		}); err != nil {
			return err
		}
		if !accepted {
			return ErrHostKeyRejected
		}
		return saveHostKey(ctx, o, address, remote, key)
	}, nil
}
