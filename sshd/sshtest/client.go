package sshtest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"golang.org/x/crypto/ssh"
)

// Client represents an SSH client for testing.
type Client interface {
	// Connect establishes the SSH connection.
	Connect() error

	// Close closes the connection.
	Close() error

	// IsConnected returns true if connected.
	IsConnected() bool

	// Shell starts an interactive shell session.
	Shell() (Session, error)

	// Exec executes a command and returns the result.
	Exec(cmd string) (*ExecResult, error)

	// SFTP returns an SFTP client.
	SFTP() (*SFTPClient, error)

	// LocalForward creates a local port forward.
	LocalForward(localAddr, remoteAddr string) (net.Listener, error)

	// RemoteForward creates a remote port forward. The server-side listener is
	// owned by the client and is closed by Client.Close.
	RemoteForward(remoteAddr, localAddr string) error

	// Events returns the event bus.
	Events() *EventBus

	// Name returns the client name.
	Name() string
}

// Session represents an interactive shell session.
type Session interface {
	io.Reader
	io.Writer
	io.Closer

	// Resize changes the terminal size.
	Resize(cols, rows uint32) error

	// Output returns all captured output.
	Output() string

	// WaitForOutput waits for specific text in output.
	WaitForOutput(text string, timeout time.Duration) error
}

// ExecResult contains the result of command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// clientGo implements Client using Go's SSH library.
type clientGo struct {
	config     *clientConfig
	sshClient  *ssh.Client
	events     *EventBus
	mu         sync.Mutex
	connected  bool
	serverAddr string

	forwardMu        sync.Mutex
	forwardListeners map[net.Listener]struct{}
}

// NewClient creates a new test client.
func NewClient(serverAddr string, opts ...ClientOption) (Client, error) {
	cfg := defaultClientConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	c := &clientGo{
		config:           cfg,
		serverAddr:       serverAddr,
		forwardListeners: make(map[net.Listener]struct{}),
	}

	// Set up events
	if cfg.events != nil {
		c.events = cfg.events
	} else {
		c.events = NewEventBus()
	}

	// Generate key from seed if needed
	if cfg.keySeed != "" && cfg.key == nil {
		signer, err := key.SignerFromSeed(cfg.keySeed)
		if err != nil {
			return nil, fmt.Errorf("failed to generate key from seed: %w", err)
		}
		cfg.key = signer
	}

	return c, nil
}

// Name returns the client name.
func (c *clientGo) Name() string {
	return c.config.name
}

// Events returns the event bus.
func (c *clientGo) Events() *EventBus {
	return c.events
}

// Connect establishes the SSH connection.
func (c *clientGo) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return fmt.Errorf("already connected")
	}

	// Build auth methods
	var authMethods []ssh.AuthMethod
	if c.config.key != nil {
		authMethods = append(authMethods, ssh.PublicKeys(c.config.key))
	}
	if c.config.password != "" {
		authMethods = append(authMethods, ssh.Password(c.config.password))
	}

	if len(authMethods) == 0 && !c.config.noAuth {
		return fmt.Errorf("no authentication method configured")
	}

	sshConfig := &ssh.ClientConfig{
		User:            c.config.user,
		Auth:            authMethods, // Empty slice triggers "none" auth
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         c.config.timeout,
	}

	client, err := ssh.Dial("tcp", c.serverAddr, sshConfig)
	if err != nil {
		c.events.Emit(scenario.EventAuthFailure, "client", c.config.name, "error", err.Error())
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.sshClient = client
	c.connected = true
	c.events.Emit(scenario.EventConnected, "client", c.config.name, "addr", c.serverAddr)
	c.events.Emit(scenario.EventAuthSuccess, "client", c.config.name, "user", c.config.user)

	return nil
}

// Close closes the connection.
func (c *clientGo) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return nil
	}
	client := c.sshClient
	c.connected = false
	c.sshClient = nil
	c.forwardMu.Lock()
	listeners := make([]net.Listener, 0, len(c.forwardListeners))
	for listener := range c.forwardListeners {
		listeners = append(listeners, listener)
		delete(c.forwardListeners, listener)
	}
	c.forwardMu.Unlock()

	var closeErr error
	for _, listener := range listeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = errors.Join(closeErr, fmt.Errorf("close forwarding listener %s: %w", listener.Addr(), err))
		}
	}
	if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		closeErr = errors.Join(closeErr, err)
	}
	c.events.Emit(scenario.EventDisconnected, "client", c.config.name)
	return closeErr
}

// IsConnected returns true if connected.
func (c *clientGo) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}
