package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

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
