package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"strings"
)

// Bounded trust snapshot adapted from the preserved Tigo #59 source. Crypto
// parsing and certificate validation remain in upstream x/crypto/ssh.
type sshTrustEntry struct {
	marker   string
	patterns []sshTrustPattern
	key      knownhosts.KnownKey
}
type sshTrustPattern struct {
	host, port string
	salt, hash []byte
	negative   bool
}

func trustSnapshot(ctx context.Context, paths []string, lookup string) (ssh.HostKeyCallback, error) {
	var entries []sshTrustEntry
	total := 0
	for _, path := range paths {
		if path == "" {
			continue
		}
		data, err := readSource(ctx, path, 4<<20)
		if err != nil {
			return nil, fmt.Errorf("ssh client: trust read: %w", err)
		}
		total += len(data)
		if total > 4<<20 || bytes.Count(data, []byte{10}) > 65536 {
			return nil, errors.New("ssh client: trust input limit")
		}
		for index, line := range bytes.Split(data, []byte{'\n'}) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			// ParseKnownHosts has a limited comment field grammar. Trust consumes
			// only marker/hosts/type/blob; arbitrary trailing comments remain inert.
			fields := bytes.Fields(line)
			keyFields := 3
			if len(fields) > 0 && len(fields[0]) > 0 && fields[0][0] == '@' {
				keyFields = 4
			}
			if len(fields) < keyFields {
				return nil, errors.New("ssh client: invalid SSH trust entry")
			}
			line = bytes.Join(fields[:keyFields], []byte{' '})
			marker, hosts, key, _, _, err := ssh.ParseKnownHosts(line)
			if err != nil {
				return nil, fmt.Errorf("ssh client: trust parse: %w", err)
			}
			if marker != "" && marker != "revoked" && marker != "cert-authority" {
				return nil, errors.New("ssh client: invalid SSH trust marker")
			}
			entry := sshTrustEntry{marker: marker, key: knownhosts.KnownKey{Key: key, Filename: path, Line: index + 1}}
			for _, host := range hosts {
				pattern, err := parseSSHTrustPattern(host)
				if err != nil {
					return nil, fmt.Errorf("ssh client: trust parse: %w", err)
				}
				entry.patterns = append(entry.patterns, pattern)
			}
			entries = append(entries, entry)
		}
	}
	// Public parsers/checker retain certificate validation. The owned table is
	// authored here over pinned bytes; knownhosts.New would reopen input paths.
	revoked := make(map[string]knownhosts.KnownKey)
	for _, entry := range entries {
		if entry.marker == "revoked" {
			revoked[sshPlainKey(entry.key.Key)] = entry.key
		}
	}
	checker := &ssh.CertChecker{
		IsHostAuthority: func(key ssh.PublicKey, address string) bool {
			work := 1 << 20
			for _, entry := range entries {
				if entry.marker == "cert-authority" && bytes.Equal(entry.key.Key.Marshal(), key.Marshal()) {
					yes, err := entry.matches(address, &work)
					if err != nil {
						return false
					}
					if yes {
						return true
					}
				}
			}
			return false
		},
		IsRevoked: func(cert *ssh.Certificate) bool {
			_, keyRevoked := revoked[sshPlainKey(cert)]
			_, caRevoked := revoked[sshPlainKey(cert.SignatureKey)]
			return keyRevoked || caRevoked
		},
		HostKeyFallback: func(address string, _ net.Addr, key ssh.PublicKey) error {
			if record, set := revoked[sshPlainKey(key)]; set {
				return &knownhosts.RevokedError{Revoked: record}
			}
			mismatch := &knownhosts.KeyError{}
			work := 1 << 20
			for _, entry := range entries {
				if entry.marker == "revoked" {
					continue
				}
				yes, err := entry.matches(address, &work)
				if err != nil {
					return err
				}
				if !yes {
					continue
				}
				mismatch.Want = append(mismatch.Want, entry.key)
				if entry.marker == "" && bytes.Equal(entry.key.Key.Marshal(), key.Marshal()) {
					return nil
				}
			}
			return mismatch
		},
	}
	return func(_ string, remote net.Addr, key ssh.PublicKey) error {
		return checker.CheckHostKey(lookup, remote, key)
	}, nil
}
func sshPlainKey(key ssh.PublicKey) string {
	if cert, ok := key.(*ssh.Certificate); ok {
		key = cert.Key
	}
	return string(key.Marshal())
}
func parseSSHTrustPattern(value string) (sshTrustPattern, error) {
	var out sshTrustPattern
	if strings.HasPrefix(value, "|") {
		fields := strings.Split(value, "|")
		if len(fields) != 4 || fields[1] != "1" {
			return out, errors.New("invalid SSH hostname hash")
		}
		var err error
		out.salt, err = base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			return out, err
		}
		out.hash, err = base64.StdEncoding.DecodeString(fields[3])
		if err != nil || len(out.hash) != sha1.Size || len(out.salt) != sha1.Size {
			return out, errors.New("invalid SSH hostname hash")
		}
		return out, nil
	}
	out.negative = strings.HasPrefix(value, "!")
	value = strings.TrimPrefix(value, "!")
	if value == "" {
		return out, errors.New("empty SSH hostname pattern")
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		if strings.HasPrefix(value, "[") {
			return out, err
		}
		host, port = value, "22"
	}
	out.host, out.port = strings.ToLower(host), port
	return out, nil
}
func (entry sshTrustEntry) matches(address string, work *int) (bool, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false, err
	}
	positive := false
	for _, pattern := range entry.patterns {
		matched := false
		if pattern.hash != nil {
			*work -= len(address) + len(pattern.salt)
			if *work < 0 {
				return false, errors.New("ssh client: SSH trust matching work limit")
			}
			mac := hmac.New(sha1.New, pattern.salt)
			_, _ = mac.Write([]byte(knownhosts.Normalize(address)))
			matched = hmac.Equal(mac.Sum(nil), pattern.hash)
		} else if pattern.port == port {
			matched, err = sshGlob(pattern.host, strings.ToLower(host), work)
			if err != nil {
				return false, err
			}
		}
		if matched && pattern.negative {
			return false, nil
		}
		positive = positive || matched
	}
	return positive, nil
}
