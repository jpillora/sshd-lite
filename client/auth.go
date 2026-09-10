// Package client implements the sshd-lite SSH client.
package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/internal/sshconn"
	"github.com/jpillora/sshd-lite/internal/termio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func (c Config) knownHostsPath() (string, error) {
	if c.KnownHosts != "" && c.ShareKnownHosts {
		return "", errors.New("--known-hosts and --share-known-hosts cannot be used together")
	}
	if c.KnownHosts != "" {
		return expandHome(c.KnownHosts), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	if c.ShareKnownHosts {
		return filepath.Join(home, ".ssh", "known_hosts"), nil
	}
	if state := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(state) {
		return filepath.Join(state, "sshd-lite", "known_hosts"), nil
	}
	return filepath.Join(home, ".local", "state", "sshd-lite", "known_hosts"), nil
}

func promptPassword(prompt string) (string, error) {
	tty, err := termio.OpenTTY()
	if err != nil {
		return "", fmt.Errorf("%s: no terminal; use --password or --identity", prompt)
	}
	defer tty.Close()
	out, err := termio.OpenTTYOutput()
	if err != nil {
		return "", err
	}
	defer out.Close()
	fmt.Fprint(out, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(out)
	return string(b), err
}

func knownHostsCallback(filename string, accept, strict bool) (ssh.HostKeyCallback, error) {
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
			confirmed, promptErr := confirmHostKey(hostname, key, mismatch)
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

func confirmHostKey(hostname string, key ssh.PublicKey, mismatch bool) (bool, error) {
	tty, err := termio.OpenTTY()
	if err != nil {
		return false, errors.New("no terminal available; use --accept to confirm non-interactively")
	}
	defer tty.Close()
	out, err := termio.OpenTTYOutput()
	if err != nil {
		return false, err
	}
	defer out.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return false, errors.New("host-key confirmation requires a terminal; use --accept")
	}
	writeHostKeyPrompt(out, hostname, key, mismatch)
	return readConfirmation(tty)
}

func writeHostKeyPrompt(w io.Writer, hostname string, key ssh.PublicKey, mismatch bool) {
	fingerprint := ssh.FingerprintSHA256(key)
	if mismatch {
		fmt.Fprintln(w, "\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@")
		fmt.Fprintln(w, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!")
		fmt.Fprintln(w, "This could mean that someone is intercepting your connection.")
		fmt.Fprintf(w, "New key for %s is %s %s.\n", hostname, key.Type(), fingerprint)
		fmt.Fprint(w, "Replace the saved key? (y/N) ")
	} else {
		fmt.Fprintf(w, "The authenticity of host %s cannot be established.\n", hostname)
		fmt.Fprintf(w, "%s key fingerprint is %s.\n", key.Type(), fingerprint)
		fmt.Fprint(w, "Add this host to known_hosts? (y/N) ")
	}
}

func readConfirmation(r io.Reader) (bool, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func updateKnownHosts(filename, hostname string, key ssh.PublicKey, stale []knownhosts.KnownKey) error {
	return updateKnownHostsEntry(filename, hostname, key, stale, "")
}

func updateKnownHostsEntry(filename, hostname string, key ssh.PublicKey, stale []knownhosts.KnownKey, marker string) error {
	configuredName := filename
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		filename, err = filepath.EvalSymlinks(filename)
		if err != nil {
			return fmt.Errorf("resolve known hosts symlink: %w", err)
		}
	}
	data, err := os.ReadFile(filename)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read known hosts: %w", err)
	}
	mode := fs.FileMode(0o600)
	if info, statErr := os.Stat(filename); statErr == nil {
		mode = info.Mode().Perm()
	}
	remove := make(map[int]struct{})
	for _, known := range stale {
		if sameFile(known.Filename, configuredName) || sameFile(known.Filename, filename) {
			remove[known.Line] = struct{}{}
		}
	}
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		if _, drop := remove[i+1]; drop {
			if remainder, ok := preserveOtherHosts(line, hostname); ok {
				kept = append(kept, remainder)
			}
			continue
		}
		if i == len(lines)-1 && line == "" {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept, marker+knownhosts.Line([]string{hostname}, key))
	content := []byte(strings.Join(kept, "\n") + "\n")

	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create known hosts directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".known-hosts-*")
	if err != nil {
		return fmt.Errorf("create known hosts update: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write known hosts update: %w", err)
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("replace known hosts: %w", err)
	}
	return nil
}

// A known_hosts line can bind one key to several comma-separated hosts. When
// the changed host is an explicit member, retain the other bindings. Hashed and
// wildcard matches retain their other bindings by excluding the changed host.
// Hashed matches cannot be separated safely, so those lines are removed before
// the replacement is appended.
func preserveOtherHosts(line, hostname string) (string, bool) {
	fields := strings.Fields(line)
	hostField := 0
	if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
		hostField = 1
	}
	if len(fields) <= hostField {
		return "", false
	}
	target := knownhosts.Normalize(hostname)
	hosts := strings.Split(fields[hostField], ",")
	if strings.HasPrefix(fields[hostField], "|") {
		return "", false
	}
	remaining := hosts[:0]
	removed := false
	for _, host := range hosts {
		if host == target || host == hostname {
			removed = true
			continue
		}
		remaining = append(remaining, host)
	}
	if removed && len(remaining) == 0 {
		return "", false
	}
	// The stale key may still match through a remaining wildcard. An explicit
	// negation ensures the replacement wins while retaining every other binding.
	remaining = append([]string{"!" + target}, remaining...)
	fields[hostField] = strings.Join(remaining, ",")
	return strings.Join(fields, " "), true
}

func sameFile(a, b string) bool {
	if aa, errA := os.Stat(a); errA == nil {
		if bb, errB := os.Stat(b); errB == nil {
			return os.SameFile(aa, bb)
		}
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func (c Config) connect(ctx context.Context) (*ssh.Client, error) {
	username, address, err := c.target()
	if err != nil {
		return nil, err
	}
	var hostKey ssh.HostKeyCallback
	if c.Insecure {
		hostKey = ssh.InsecureIgnoreHostKey()
	} else {
		path, pathErr := c.knownHostsPath()
		if pathErr != nil {
			return nil, pathErr
		}
		hostKey, err = knownHostsCallback(path, c.Accept, c.StrictHosts)
		if err != nil {
			return nil, fmt.Errorf("load known hosts (use --known-hosts or explicitly --insecure): %w", err)
		}
	}
	var auth []ssh.AuthMethod
	var signers []ssh.Signer
	var agentSigners func() ([]ssh.Signer, error)
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		agentConn, err := net.DialTimeout("unix", socket, 3*time.Second)
		if err == nil {
			defer agentConn.Close()
			agentSigners = agent.NewClient(agentConn).Signers
		}
	}
	paths := []string{c.Identity}
	if c.Identity == "" {
		paths = []string{"~/.ssh/id_ed25519", "~/.ssh/id_rsa"}
	}
	for _, path := range paths {
		b, err := os.ReadFile(expandHome(path))
		if err != nil {
			if c.Identity != "" {
				return nil, err
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(b)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && c.Identity != "" {
			pass, promptErr := promptPassword("Private key passphrase: ")
			if promptErr != nil {
				return nil, promptErr
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass))
		}
		if err != nil {
			if c.Identity != "" {
				return nil, err
			}
			continue
		}
		signers = append(signers, signer)
	}
	// Go SSH tries each authentication method once. Combine explicit keys and
	// agent keys so a loaded agent cannot hide the requested identity.
	auth = append(auth, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		keys := append([]ssh.Signer(nil), signers...)
		if agentSigners != nil {
			more, err := agentSigners()
			if err == nil {
				keys = append(keys, more...)
			}
		}
		return keys, nil
	}))
	auth = append(auth, ssh.PasswordCallback(func() (string, error) {
		if c.Password != "" {
			return c.Password, nil
		}
		return promptPassword(username + "@" + address + " password: ")
	}))
	return sshconn.Dial(ctx, address, &ssh.ClientConfig{User: username, Auth: auth, HostKeyCallback: hostKey})
}
