package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Legacy Connect trust policy supports automatic first use and confirmed or
// explicitly accepted replacement. Dial uses the stricter trust.go policy.
func knownHostsCallback(filename string, accept, strict bool) (ssh.HostKeyCallback, error) {
	return knownHostsCallbackContext(context.Background(), filename, accept, strict)
}

func knownHostsCallbackContext(ctx context.Context, filename string, accept, strict bool) (ssh.HostKeyCallback, error) {
	filename = expandHome(filename)
	check, err := knownhosts.New(filename)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		check = func(string, net.Addr, ssh.PublicKey) error { return &knownhosts.KeyError{} }
	}
	var updateMu sync.Mutex
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		entryKey := key
		marker := ""
		isKeyError := errors.As(err, &keyErr)
		if cert, ok := key.(*ssh.Certificate); ok {
			// knownhosts reports an unrecognized host-certificate authority as an
			// untyped "no authorities" error. Check its signing key against the
			// same database to recover unknown-versus-mismatch semantics.
			if !isKeyError {
				authorityErr := check(hostname, remote, cert.SignatureKey)
				isKeyError = errors.As(authorityErr, &keyErr)
				if !isKeyError {
					// A known or revoked signing key, or another callback failure, must
					// not be promoted silently into a trusted certificate authority.
					if authorityErr != nil {
						return authorityErr
					}
					return err
				}
			}
			revoked, revokeErr := certificateRevoked(filename, cert)
			if revokeErr != nil {
				return revokeErr
			}
			validator := ssh.CertChecker{
				IsHostAuthority: func(authority ssh.PublicKey, _ string) bool {
					return bytes.Equal(authority.Marshal(), cert.SignatureKey.Marshal())
				},
				IsRevoked: func(*ssh.Certificate) bool { return revoked },
			}
			if validationErr := validator.CheckHostKey(hostname, remote, cert); validationErr != nil {
				return validationErr
			}
			entryKey = cert.SignatureKey
			marker = "@cert-authority "
		}
		if !isKeyError {
			// Revoked keys and malformed callback failures are never overridable.
			return err
		}
		mismatch := len(keyErr.Want) > 0
		// Trust on first use is the default for sshd-lite's private file. A
		// changed key remains an interactive, prominent warning unless the
		// caller explicitly supplied --accept. --strict-hosts restores the
		// confirmation step for a previously unknown host.
		if !accept && (mismatch || strict) {
			confirmed, promptErr := confirmHostKeyContext(ctx, hostname, key, mismatch)
			if promptErr != nil {
				return fmt.Errorf("verify host key: %w: %v", err, promptErr)
			}
			if !confirmed {
				return err
			}
		}
		updateMu.Lock()
		defer updateMu.Unlock()
		return updateKnownHostsEntry(filename, hostname, entryKey, keyErr.Want, marker)
	}, nil
}

func certificateRevoked(filename string, cert *ssh.Certificate) (bool, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read known hosts revocations: %w", err)
	}
	for len(data) > 0 {
		marker, _, key, _, rest, parseErr := ssh.ParseKnownHosts(data)
		if errors.Is(parseErr, io.EOF) {
			return false, nil
		}
		if parseErr != nil {
			return false, parseErr
		}
		if marker == "revoked" && (bytes.Equal(key.Marshal(), cert.Marshal()) || bytes.Equal(key.Marshal(), cert.SignatureKey.Marshal())) {
			return true, nil
		}
		data = rest
	}
	return false, nil
}
