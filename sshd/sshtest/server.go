package sshtest

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/jpillora/sshd-lite/sshd"
	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/sshd/sshtest/log"
	"golang.org/x/crypto/ssh"
)

// Server represents an SSH server for testing.
type Server interface {
	// Start starts the server. Must be called before connecting clients.
	Start(ctx context.Context) error

	// Stop stops the server gracefully.
	Stop() error

	// Addr returns the full address (host:port) of the server.
	Addr() string

	// Host returns the host the server is listening on.
	Host() string

	// Port returns the port the server is listening on.
	Port() int

	// AddAuthorizedKey adds a public key for authentication.
	AddAuthorizedKey(name string, key ssh.PublicKey)

	// HostKey returns the server's host key.
	HostKey() ssh.PublicKey

	// Events returns the event bus for this server.
	Events() *EventBus
}

// ServerOption configures a server.
type ServerOption func(*serverConfig)

type serverConfig struct {
	sshd.Config
	authKeyMap map[string]ssh.PublicKey // name -> key for lookup
	logCapture *log.Capture
	events     *EventBus
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		Config: sshd.Config{
			Host:      "127.0.0.1",
			Port:      "0", // auto-assign
			KeySeed:   "test-server-key",
			KeySeedEC: true,
			LogQuiet:  true,
		},
		authKeyMap: make(map[string]ssh.PublicKey),
	}
}

// ServerWithPort sets the port to listen on. 0 means auto-assign.
func ServerWithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.Port = fmt.Sprintf("%d", port)
	}
}

// ServerWithHost sets the host to listen on.
func ServerWithHost(host string) ServerOption {
	return func(c *serverConfig) {
		c.Host = host
	}
}

// ServerWithSFTP enables or disables SFTP.
func ServerWithSFTP(enabled bool) ServerOption {
	return func(c *serverConfig) {
		c.SFTP = enabled
	}
}

// ServerWithTCPForwarding enables or disables TCP forwarding.
func ServerWithTCPForwarding(enabled bool) ServerOption {
	return func(c *serverConfig) {
		c.TCPForwarding = enabled
	}
}

// ServerWithShell sets the shell to use.
func ServerWithShell(shell string) ServerOption {
	return func(c *serverConfig) {
		c.Shell = shell
	}
}

// ServerWithKeySeed sets the seed for deterministic host key generation.
func ServerWithKeySeed(seed string) ServerOption {
	return func(c *serverConfig) {
		c.KeySeed = seed
	}
}

// ServerWithPassword adds password authentication for a user.
func ServerWithPassword(user, password string) ServerOption {
	return func(c *serverConfig) {
		c.AuthType = user + ":" + password
	}
}

// ServerWithNoAuth disables authentication (allows any connection).
func ServerWithNoAuth() ServerOption {
	return func(c *serverConfig) {
		c.AuthType = "none"
	}
}

// ServerWithLogger sets the log capture for the server.
func ServerWithLogger(logger *log.Capture) ServerOption {
	return func(c *serverConfig) {
		c.logCapture = logger
	}
}

// ServerWithEvents sets the event bus for the server.
func ServerWithEvents(events *EventBus) ServerOption {
	return func(c *serverConfig) {
		c.events = events
	}
}

// serverLite wraps the sshd-lite server for testing.
type serverLite struct {
	config   *serverConfig
	server   *sshd.Server
	listener net.Listener
	events   *EventBus
	hostKey  ssh.PublicKey

	mu       sync.Mutex
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	doneCh   chan struct{}
	authKeys map[string]ssh.PublicKey
}

// NewServer creates a new test server with the given options.
func NewServer(opts ...ServerOption) (Server, error) {
	cfg := defaultServerConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	s := &serverLite{
		config:   cfg,
		authKeys: make(map[string]ssh.PublicKey),
		doneCh:   make(chan struct{}),
	}

	// Copy pre-configured auth keys
	for name, k := range cfg.authKeyMap {
		s.authKeys[name] = k
	}

	// Set up events
	if cfg.events != nil {
		s.events = cfg.events
	} else {
		s.events = NewEventBus()
	}

	return s, nil
}

// AddAuthorizedKey adds a public key for authentication.
func (s *serverLite) AddAuthorizedKey(name string, key ssh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authKeys[name] = key
}

// Start starts the server.
func (s *serverLite) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}
	s.started = true
	s.mu.Unlock()

	// Create listener
	addr := fmt.Sprintf("%s:%s", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	// Build auth keys list
	s.mu.Lock()
	for _, k := range s.authKeys {
		s.config.AuthKeys = append(s.config.AuthKeys, k)
	}
	s.mu.Unlock()

	// Update port with actual listener port
	s.config.Port = fmt.Sprintf("%d", s.listener.Addr().(*net.TCPAddr).Port)

	// Set up logger
	if s.config.logCapture != nil {
		s.config.Logger = s.config.logCapture.Logger()
	}

	// Create server
	server, err := sshd.NewServer(s.config.Config)
	if err != nil {
		s.listener.Close()
		return fmt.Errorf("failed to create server: %w", err)
	}
	s.server = server

	// Get host key
	signer, err := key.SignerFromSeed(s.config.KeySeed)
	if err == nil {
		s.hostKey = signer.PublicKey()
	}

	// Create cancellable context
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Start server in background
	go func() {
		defer close(s.doneCh)
		s.server.StartWithContext(s.ctx, s.listener)
	}()

	s.events.Emit("server.started", "addr", s.Addr())
	return nil
}

// Stop stops the server.
func (s *serverLite) Stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	// Wait for server to stop
	<-s.doneCh

	s.events.Emit("server.stopped")
	return nil
}

// Addr returns the full address.
func (s *serverLite) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Host returns the host.
func (s *serverLite) Host() string {
	return s.config.Host
}

// Port returns the port.
func (s *serverLite) Port() int {
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

// HostKey returns the server's host key.
func (s *serverLite) HostKey() ssh.PublicKey {
	return s.hostKey
}

// Events returns the event bus.
func (s *serverLite) Events() *EventBus {
	return s.events
}
