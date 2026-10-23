package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Config.connect owns CLI defaults and adapts them to the explicit Dial boundary.
// Its environment inheritance and trust policy are deliberately legacy choices.
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
		hostKey, err = knownHostsCallbackContext(ctx, path, c.Accept, c.StrictHosts)
		if err != nil {
			return nil, fmt.Errorf("load known hosts (use --known-hosts or explicitly --insecure): %w", err)
		}
	}

	home, _ := os.UserHomeDir()
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sources := Sources{Home: home, Directory: directory, AgentSocket: os.Getenv("SSH_AUTH_SOCK")}
	var identities []string
	if c.Identity != "" {
		p := expandHome(c.Identity)
		if _, e := os.Stat(p); e != nil {
			return nil, e
		}
		identities = []string{p}
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	return Dial(ctx, DialOptions{Host: host, Port: port, User: username, Sources: sources, IdentityFiles: identities, Password: c.Password,
		HostKeyCallback: hostKey, Interaction: legacyInteraction(), passwordCallback: func() (string, error) {
			b, e := legacyTerminalRead(ctx, username+"@"+address+" password: ", true)
			defer clear(b)
			return string(b), e
		}})
}
