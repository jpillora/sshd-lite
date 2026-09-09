package sshtest

import (
	"time"

	"golang.org/x/crypto/ssh"
)

// ClientOption configures a client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	name     string
	user     string
	password string
	keySeed  string
	key      ssh.Signer
	noAuth   bool
	ptySize  *ptySize
	events   *EventBus
	timeout  time.Duration
}

type ptySize struct {
	cols uint32
	rows uint32
}

func defaultClientConfig() *clientConfig {
	return &clientConfig{
		user:    "user",
		timeout: 10 * time.Second,
	}
}

// ClientWithName sets the client name (for identification in tests).
func ClientWithName(name string) ClientOption {
	return func(c *clientConfig) {
		c.name = name
	}
}

// ClientWithUser sets the SSH user.
func ClientWithUser(user string) ClientOption {
	return func(c *clientConfig) {
		c.user = user
	}
}

// ClientWithPassword sets password authentication.
func ClientWithPassword(password string) ClientOption {
	return func(c *clientConfig) {
		c.password = password
	}
}

// ClientWithKey sets the SSH key for authentication.
func ClientWithKey(key ssh.Signer) ClientOption {
	return func(c *clientConfig) {
		c.key = key
	}
}

// ClientWithKeySeed sets deterministic key generation from a seed.
func ClientWithKeySeed(seed string) ClientOption {
	return func(c *clientConfig) {
		c.keySeed = seed
	}
}

// ClientWithPTY enables PTY with the specified size.
func ClientWithPTY(cols, rows uint32) ClientOption {
	return func(c *clientConfig) {
		c.ptySize = &ptySize{cols: cols, rows: rows}
	}
}

// ClientWithEvents sets the event bus for the client.
func ClientWithEvents(events *EventBus) ClientOption {
	return func(c *clientConfig) {
		c.events = events
	}
}

// ClientWithTimeout sets the connection timeout.
func ClientWithTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = d
	}
}

// ClientWithNoAuth allows connecting without authentication (for servers with auth=none).
func ClientWithNoAuth() ClientOption {
	return func(c *clientConfig) {
		c.noAuth = true
	}
}
