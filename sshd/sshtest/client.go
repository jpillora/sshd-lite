package sshtest

import (
	"fmt"
	"io"
	"net"
	"strings"
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

	// RemoteForward creates a remote port forward.
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

// SFTPClient wraps SFTP operations (placeholder for now).
type SFTPClient struct {
	client *clientGo
}

// ClientOption configures a client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	name     string
	host     string
	port     int
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

// clientGo implements Client using Go's SSH library.
type clientGo struct {
	config     *clientConfig
	sshClient  *ssh.Client
	events     *EventBus
	mu         sync.Mutex
	connected  bool
	serverAddr string
}

// NewClient creates a new test client.
func NewClient(serverAddr string, opts ...ClientOption) (Client, error) {
	cfg := defaultClientConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	c := &clientGo{
		config:     cfg,
		serverAddr: serverAddr,
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

	err := c.sshClient.Close()
	c.connected = false
	c.events.Emit(scenario.EventDisconnected, "client", c.config.name)
	return err
}

// IsConnected returns true if connected.
func (c *clientGo) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Shell starts an interactive shell session.
func (c *clientGo) Shell() (Session, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Set up PTY if configured
	cols, rows := uint32(80), uint32(24)
	if c.config.ptySize != nil {
		cols = c.config.ptySize.cols
		rows = c.config.ptySize.rows
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", int(rows), int(cols), modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to request PTY: %w", err)
	}

	c.events.Emit(scenario.EventPTYRequested, "client", c.config.name, "cols", fmt.Sprint(cols), "rows", fmt.Sprint(rows))

	// Get stdin/stdout pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	c.events.Emit(scenario.EventShellStarted, "client", c.config.name)

	sess := &sessionGo{
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		output:  &outputCapture{},
		events:  c.events,
		name:    c.config.name,
		cols:    cols,
		rows:    rows,
	}

	// Start capturing output
	go sess.captureOutput()

	return sess, nil
}

// Exec executes a command and returns the result.
func (c *clientGo) Exec(cmd string) (*ExecResult, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	c.events.Emit(scenario.EventExecStarted, "client", c.config.name, "command", cmd)

	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			return nil, fmt.Errorf("failed to run command: %w", err)
		}
	}

	c.events.Emit(scenario.EventExecCompleted, "client", c.config.name, "command", cmd, "exit_code", fmt.Sprint(result.ExitCode))

	return result, nil
}

// SFTP returns an SFTP client.
func (c *clientGo) SFTP() (*SFTPClient, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	c.mu.Unlock()

	c.events.Emit(scenario.EventSFTPStarted, "client", c.config.name)
	return &SFTPClient{client: c}, nil
}

// LocalForward creates a local port forward.
func (c *clientGo) LocalForward(localAddr, remoteAddr string) (net.Listener, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", localAddr, err)
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "local", "local", localAddr, "remote", remoteAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(conn net.Conn) {
				defer conn.Close()

				remote, err := client.Dial("tcp", remoteAddr)
				if err != nil {
					return
				}
				defer remote.Close()

				// Bidirectional copy
				done := make(chan struct{}, 2)
				go func() {
					io.Copy(remote, conn)
					done <- struct{}{}
				}()
				go func() {
					io.Copy(conn, remote)
					done <- struct{}{}
				}()
				<-done
			}(conn)
		}
	}()

	return listener, nil
}

// RemoteForward creates a remote port forward.
func (c *clientGo) RemoteForward(remoteAddr, localAddr string) error {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		return fmt.Errorf("failed to request remote forward: %w", err)
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "remote", "remote", remoteAddr, "local", localAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(conn net.Conn) {
				defer conn.Close()

				local, err := net.Dial("tcp", localAddr)
				if err != nil {
					return
				}
				defer local.Close()

				done := make(chan struct{}, 2)
				go func() {
					io.Copy(local, conn)
					done <- struct{}{}
				}()
				go func() {
					io.Copy(conn, local)
					done <- struct{}{}
				}()
				<-done
			}(conn)
		}
	}()

	return nil
}

// sessionGo implements Session.
type sessionGo struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	output  *outputCapture
	events  *EventBus
	name    string
	cols    uint32
	rows    uint32
}

// outputCapture captures output in a thread-safe way.
type outputCapture struct {
	mu   sync.RWMutex
	data []byte
}

func (o *outputCapture) Write(p []byte) (n int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = append(o.data, p...)
	return len(p), nil
}

func (o *outputCapture) String() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return string(o.data)
}

func (s *sessionGo) captureOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.stdout.Read(buf)
		if n > 0 {
			s.output.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *sessionGo) Read(p []byte) (n int, err error) {
	return s.stdout.Read(p)
}

func (s *sessionGo) Write(p []byte) (n int, err error) {
	return s.stdin.Write(p)
}

func (s *sessionGo) Close() error {
	s.events.Emit(scenario.EventShellEnded, "client", s.name)
	return s.session.Close()
}

func (s *sessionGo) Resize(cols, rows uint32) error {
	s.cols = cols
	s.rows = rows
	err := s.session.WindowChange(int(rows), int(cols))
	if err == nil {
		s.events.Emit(scenario.EventPTYResized, "client", s.name, "cols", fmt.Sprint(cols), "rows", fmt.Sprint(rows))
	}
	return err
}

func (s *sessionGo) Output() string {
	return s.output.String()
}

func (s *sessionGo) WaitForOutput(text string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Output(), text) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %q in output", text)
}
