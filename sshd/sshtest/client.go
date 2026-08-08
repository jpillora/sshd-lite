package sshtest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"github.com/pkg/sftp"
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

// SFTPClient owns one SFTP subsystem session. Upload and Download may be used
// concurrently (the underlying pkg/sftp client supports concurrent requests).
// Callers ordinarily close it after operations finish. Close atomically prevents
// a pending Download from replacing its destination, then closes the underlying
// session to interrupt in-flight requests. A Download already committing its
// rename wins that race and completes normally. Upload cancellation can leave a
// partial remote file. Close is idempotent and returns the first close result.
type SFTPClient struct {
	client      *sftp.Client
	lifecycleMu sync.Mutex
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

// Upload copies localPath to remotePath, creating or truncating the remote file.
func (c *SFTPClient) Upload(localPath, remotePath string) (retErr error) {
	if err := c.ensureOpen(); err != nil {
		return fmt.Errorf("start upload: %w", err)
	}
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local upload file %q: %w", localPath, err)
	}
	defer func() {
		if err := local.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close local upload file %q: %w", localPath, err))
		}
	}()

	remote, err := c.client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("open remote upload file %q: %w", remotePath, err)
	}
	defer func() {
		if err := remote.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close remote upload file %q: %w", remotePath, err))
		}
	}()

	if _, err := io.Copy(remote, local); err != nil {
		return fmt.Errorf("copy local file %q to remote file %q: %w", localPath, remotePath, err)
	}
	return nil
}

// Download copies remotePath to localPath. Data is first written to a temporary
// sibling and atomically renamed where supported, so a failed transfer is never
// exposed at localPath as a successful complete download.
func (c *SFTPClient) Download(remotePath, localPath string) (retErr error) {
	if err := c.ensureOpen(); err != nil {
		return fmt.Errorf("start download: %w", err)
	}
	remote, err := c.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote download file %q: %w", remotePath, err)
	}
	remoteClosed := false
	defer func() {
		if !remoteClosed {
			if err := remote.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close remote download file %q: %w", remotePath, err))
			}
		}
	}()

	dir := filepath.Dir(localPath)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(localPath)+".part-*")
	if err != nil {
		return fmt.Errorf("create temporary local download file for %q: %w", localPath, err)
	}
	tempPath := temp.Name()
	keepTemp := false
	tempClosed := false
	defer func() {
		if !tempClosed {
			if err := temp.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close temporary local download file %q: %w", tempPath, err))
			}
		}
		if !keepTemp {
			if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				retErr = errors.Join(retErr, fmt.Errorf("remove partial local download file %q: %w", tempPath, err))
			}
		}
	}()

	if _, err := io.Copy(temp, remote); err != nil {
		return fmt.Errorf("copy remote file %q to local file %q: %w", remotePath, localPath, err)
	}
	err = temp.Close()
	tempClosed = true
	if err != nil {
		return fmt.Errorf("close completed local download file %q: %w", tempPath, err)
	}
	err = remote.Close()
	remoteClosed = true
	if err != nil {
		return fmt.Errorf("close completed remote download file %q: %w", remotePath, err)
	}
	// Close and the destination commit have one explicit winner. Do not hold
	// lifecycleMu while doing protocol I/O: pkg/sftp Close may need those
	// requests to unwind.
	c.lifecycleMu.Lock()
	if c.closed {
		c.lifecycleMu.Unlock()
		return fmt.Errorf("commit local download file %q: SFTP session closed", localPath)
	}
	err = replaceFile(tempPath, localPath)
	if err == nil {
		keepTemp = true
	}
	c.lifecycleMu.Unlock()
	if err != nil {
		return fmt.Errorf("replace local download file %q: %w", localPath, err)
	}
	return nil
}

func (c *SFTPClient) ensureOpen() error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.closed {
		return errors.New("SFTP session closed")
	}
	return nil
}

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

// Close prevents future download commits and closes this SFTP subsystem
// session. It is safe to call repeatedly. Close does not hold the lifecycle
// mutex while pkg/sftp shuts down, so interrupted requests can unwind.
func (c *SFTPClient) Close() error {
	c.closeOnce.Do(func() {
		c.lifecycleMu.Lock()
		c.closed = true
		c.lifecycleMu.Unlock()
		c.closeErr = c.client.Close()
	})
	return c.closeErr
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
	client := c.sshClient
	c.mu.Unlock()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("start SFTP subsystem: %w", err)
	}
	c.events.Emit(scenario.EventSFTPStarted, "client", c.config.name)
	return &SFTPClient{client: sftpClient}, nil
}

func (c *clientGo) trackForwardListener(listener net.Listener) bool {
	// Keep connection state and listener registration in one critical section.
	// Close takes these locks in the same order before snapshotting, so a
	// listener is either included in that snapshot or rejected after disconnect.
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return false
	}
	c.forwardMu.Lock()
	c.forwardListeners[listener] = struct{}{}
	c.forwardMu.Unlock()
	return true
}

func (c *clientGo) untrackForwardListener(listener net.Listener) {
	c.forwardMu.Lock()
	delete(c.forwardListeners, listener)
	c.forwardMu.Unlock()
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
	if !c.trackForwardListener(listener) {
		_ = listener.Close()
		return nil, fmt.Errorf("connection closed while creating local forward")
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "local", "local", localAddr, "remote", remoteAddr)

	go func() {
		defer c.untrackForwardListener(listener)
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

// RemoteForward requests a remote port forward. It returns only setup errors;
// the client retains ownership of the listener until Client.Close.
func (c *clientGo) RemoteForward(remoteAddr, localAddr string) error {
	_, err := c.remoteForwardListener(remoteAddr, localAddr)
	return err
}

func (c *clientGo) remoteForwardListener(remoteAddr, localAddr string) (net.Listener, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to request remote forward: %w", err)
	}
	if !c.trackForwardListener(listener) {
		_ = listener.Close()
		return nil, fmt.Errorf("connection closed while creating remote forward")
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "remote", "remote", remoteAddr, "local", localAddr)

	go func() {
		defer c.untrackForwardListener(listener)
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

	return listener, nil
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
