package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"net"
	"sync"
	"time"
)

// authOwner keeps the agent alive through handshake signatures only.
type authOwner struct {
	conn net.Conn
	stop func() bool
	done chan struct{}
}

func (a *authOwner) close() {
	if a.conn == nil {
		return
	}
	if !a.stop() {
		<-a.done
	}
	_ = a.conn.Close()
}
func (a *authOwner) agent(ctx context.Context, socket string) ([]ssh.Signer, error) {
	if a.conn != nil {
		return nil, errors.New("ssh client: repeated agent discovery")
	}
	if socket == "" {
		return nil, nil
	}
	c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	a.conn = c
	a.done = make(chan struct{})
	a.stop = context.AfterFunc(ctx, func() { _ = c.Close(); close(a.done) })
	if err := c.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	// Hide Closer so canonical RPCs own no reader goroutine.
	rw := struct {
		io.Reader
		io.Writer
	}{io.LimitReader(c, 1<<20), c}
	signers, err := agent.NewClient(rw).Signers()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("ssh client: agent keys: %w", err)
	}
	if len(signers) > 256 {
		return nil, errors.New("ssh client: agent identity limit")
	}
	return signers, nil
}

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

// OpenSSH offers matching agent references first, remaining agent keys unless
// IdentitiesOnly, then unconsumed file identities in declaration order.
func orderedSigners(agents, files []ssh.Signer, only bool) []ssh.Signer {
	used := make([]bool, len(files))
	out := make([]ssh.Signer, 0, len(agents)+len(files))
	var other []ssh.Signer
	for _, a := range agents {
		found := -1
		for i, f := range files {
			if !used[i] && bytes.Equal(a.PublicKey().Marshal(), f.PublicKey().Marshal()) {
				found = i
				break
			}
		}
		if found >= 0 {
			used[found] = true
			out = append(out, a)
		} else if !only {
			other = append(other, a)
		}
	}
	out = append(out, other...)
	for i, f := range files {
		if !used[i] {
			out = append(out, f)
		}
	}
	return out[:len(out):len(out)]
}

// Decrypt only when the server requests a signature for this key.
type fileSigner struct {
	public  ssh.PublicKey
	data    []byte
	path    string
	ctx     context.Context
	options DialOptions
	mu      sync.Mutex
	signer  ssh.Signer
}

func (s *fileSigner) PublicKey() ssh.PublicKey { return s.public }
func (s *fileSigner) Sign(random io.Reader, data []byte) (*ssh.Signature, error) {
	return s.SignWithAlgorithm(random, data, "")
}
func (s *fileSigner) SignWithAlgorithm(random io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if s.signer == nil {
		if len(s.data) == 0 {
			return nil, errors.New("ssh client: selected public identity is not available in agent")
		}
		var err error
		s.signer, err = decryptIdentity(s.ctx, s.options, s.path, s.data)
		if err != nil {
			return nil, fmt.Errorf("ssh client: encrypted identity requires a passphrase: %w", err)
		}
	}
	if a, ok := s.signer.(ssh.AlgorithmSigner); ok {
		return a.SignWithAlgorithm(random, data, algorithm)
	}
	if algorithm != "" && algorithm != s.public.Type() {
		return nil, errors.New("ssh client: unsupported signing algorithm")
	}
	return s.signer.Sign(random, data)
}
