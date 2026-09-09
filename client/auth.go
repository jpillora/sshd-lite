// Package client implements the sshd-lite SSH client.
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func promptPassword(prompt string) (string, error) {
	tty, err := termio.OpenTTY()
	if err != nil {
		return "", fmt.Errorf("%s: no terminal; use --password or --identity", prompt)
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	return string(b), err
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
		path := c.KnownHosts
		if path == "" {
			path = "~/.ssh/known_hosts"
		}
		hostKey, err = knownhosts.New(expandHome(path))
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
