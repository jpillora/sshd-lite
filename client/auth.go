package client

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

var ErrAuthentication = errors.New("ssh client: authentication failed")

// prepareAuthentication owns one dial's authentication methods and cleanup.
func prepareAuthentication(ctx context.Context, o DialOptions) ([]ssh.AuthMethod, func()) {
	owner := &authOwner{}
	if o.Auth != nil {
		return append([]ssh.AuthMethod(nil), o.Auth...), owner.close
	}
	var methods []ssh.AuthMethod
	if o.resolved.pubkey {
		methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			agentKeys, err := owner.agent(ctx, o.Sources.AgentSocket)
			if err != nil {
				return nil, err
			}
			var files []ssh.Signer
			for _, name := range o.IdentityFiles {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				data, err := readSource(ctx, name, 1<<20)
				if err != nil {
					return nil, fmt.Errorf("ssh client: identity read: %w", err)
				}
				if data == nil {
					data, err = readSource(ctx, name+".pub", 1<<20)
					if err != nil {
						return nil, err
					}
					if data == nil {
						continue
					}
				}
				if pub, _, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
					files = append(files, &fileSigner{public: pub, path: name, ctx: ctx, options: o})
					continue
				}
				signer, err := ssh.ParsePrivateKey(data)
				if err == nil {
					files = append(files, signer)
					continue
				}
				var missing *ssh.PassphraseMissingError
				if !errors.As(err, &missing) {
					return nil, fmt.Errorf("ssh client: identity parse: %w", err)
				}
				pub := missing.PublicKey
				if pub == nil {
					raw, e := readSource(ctx, name+".pub", 1<<20)
					if e != nil {
						return nil, e
					}
					if raw != nil {
						pub, _, _, _, err = ssh.ParseAuthorizedKey(raw)
						if err != nil {
							return nil, err
						}
					}
				}
				if pub == nil {
					signer, err = decryptIdentity(ctx, o, name, data)
					if err != nil {
						return nil, err
					}
					files = append(files, signer)
					continue
				}
				files = append(files, &fileSigner{public: pub, data: data, path: name, ctx: ctx, options: o})
			}
			return orderedSigners(agentKeys, files, o.resolved.only), nil
		}))
	}
	if o.resolved.password {
		if o.Password != "" {
			methods = append(methods, ssh.Password(o.Password))
		} else if o.passwordCallback != nil {
			methods = append(methods, ssh.PasswordCallback(func() (string, error) {
				var password string
				err := o.interaction.run(ctx, func() error { var e error; password, e = o.passwordCallback(); return e })
				return password, err
			}))
		}
	}
	return methods, owner.close
}
