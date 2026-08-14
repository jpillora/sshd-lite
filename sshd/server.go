package sshd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/jpillora/jplog"
	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

// Server is a simple SSH Daemon
type Server struct {
	config     Config
	sshConfig  *ssh.ServerConfig
	handshakes chan struct{}

	// xssh configuration built from sshd config
	xsshConfig *xssh.Config
}

// NewServer creates a new Server
func NewServer(c Config) (*Server, error) {
	if err := validateHandlerConflicts(c); err != nil {
		return nil, err
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = DefaultHandshakeTimeout
	}
	if c.MaxPendingHandshakes == 0 {
		c.MaxPendingHandshakes = DefaultMaxPendingHandshakes
	}
	if l := c.Logger; l == nil && !c.LogQuiet {
		h := jplog.Handler(os.Stdout)
		if c.LogVerbose {
			h = h.Verbose()
		}
		l = slog.New(h)
		c.Logger = l
	}
	s := &Server{config: c}
	if c.MaxPendingHandshakes > 0 {
		s.handshakes = make(chan struct{}, c.MaxPendingHandshakes)
	}
	sc, err := s.computeSSHConfig()
	if err != nil {
		return nil, err
	}
	s.sshConfig = sc

	// Build xssh config
	xc := &xssh.Config{
		Logger:                 c.Logger,
		KeepAlive:              c.KeepAlive,
		IgnoreEnv:              c.IgnoreEnv,
		InheritEnv:             c.InheritEnv,
		WorkingDirectory:       c.WorkDir,
		Shell:                  s.config.Shell, // absolute path, resolved once by computeSSHConfig
		Session:                true,
		SFTP:                   c.SFTP,
		LocalForwarding:        c.TCPForwarding,
		RemoteForwarding:       c.TCPForwarding,
		GlobalRequestHandlers:  make(map[string]xssh.GlobalRequestHandler),
		ChannelHandlers:        make(map[string]xssh.ChannelHandler),
		SessionRequestHandlers: make(map[string]xssh.SessionRequestHandler),
		SubsystemHandlers:      make(map[string]xssh.SubsystemHandler),
	}

	// Register built-in channel handler for sessions
	xc.ChannelHandlers[xssh.SessionChannelType] = func(conn xssh.Conn, ch ssh.NewChannel) error {
		return conn.HandleSessionChannel(ch)
	}

	if c.TCPForwarding {
		s.infof("TCP forwarding enabled")
	}
	if c.SFTP {
		s.infof("SFTP enabled")
	}
	// Handler conflicts were validated before any setup with side effects.
	// Copy custom maps so xssh's per-connection registration and the caller's
	// maps remain independent.
	for name, h := range c.GlobalRequestHandlers {
		xc.GlobalRequestHandlers[name] = h
	}
	for name, h := range c.ChannelHandlers {
		xc.ChannelHandlers[name] = h
	}
	for name, h := range c.SessionRequestHandlers {
		xc.SessionRequestHandlers[name] = h
	}
	for name, h := range c.SubsystemHandlers {
		xc.SubsystemHandlers[name] = h
	}

	s.xsshConfig = xc
	return s, nil
}

// Config returns the server configuration.
func (s *Server) Config() Config {
	return s.config
}

// Start listening on port
func (s *Server) Start() error {
	return s.StartContext(context.Background())
}

// StartContext listening on port with context
func (s *Server) StartContext(ctx context.Context) error {
	h := s.config.Host
	p := s.config.Port
	var l net.Listener
	var err error

	//listen
	if p == "" {
		l, err = net.Listen("tcp", h+":22")
		if err != nil {
			l, err = net.Listen("tcp", h+":2200")
			if err != nil {
				return fmt.Errorf("failed to listen on 22 and 2200")
			}
		}
	} else {
		l, err = net.Listen("tcp", h+":"+p)
		if err != nil {
			return fmt.Errorf("failed to listen on %s", p)
		}
	}

	return s.StartWithContext(ctx, l)
}

// StartWith starts the server with the provided listener.
// Ignores the Host and Port in the config.
func (s *Server) StartWith(l net.Listener) error {
	return s.StartWithContext(context.Background(), l)
}

// StartWithContext starts the server with the provided listener and context.
// The server will close when the context is cancelled.
// Ignores the Host and Port in the config.
func (s *Server) StartWithContext(ctx context.Context, l net.Listener) error {
	run := newServerRun(l)
	if ctx.Err() != nil {
		run.shutdown()
		return nil
	}

	s.infof("Listening on %s...", l.Addr())
	run.watch(ctx, func() { s.infof("Closing server") })

	var acceptErr error
	var cancelled bool
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			acceptErr = err
			cancelled = ctx.Err() != nil
			break
		}
		tracked, ok := run.track(tcpConn)
		if !ok {
			// Cancellation may win immediately after Accept returns. The run is
			// already shutting down, so this transport cannot be handed off.
			_ = tcpConn.Close()
			cancelled = true
			break
		}
		if !s.acquireHandshake() {
			s.debugf("Rejecting connection from %s: too many pending SSH handshakes", tcpConn.RemoteAddr())
			if err := tracked.Close(); err != nil {
				s.debugf("Failed to close rejected connection from %s: %s", tcpConn.RemoteAddr(), err)
			}
			run.untrack(tracked)
			continue
		}
		go func() {
			defer run.untrack(tracked)
			s.handleConn(ctx, tracked)
		}()
	}

	run.stopWatching()
	run.shutdown()
	run.wait()
	if cancelled {
		return nil
	}
	if acceptErr == nil {
		// The only error-free way out of the loop is track rejecting an
		// accepted connection because shutdown had already started.
		return nil
	}
	return fmt.Errorf("accept failed: %w", acceptErr)
}

// serverRun owns only resources accepted by one StartWithContext invocation.
// Keeping this state off Server makes concurrent invocations and HandleConn
// independent: stopping one listener cannot close another run's transports.
type serverRun struct {
	listener net.Listener

	mu        sync.Mutex
	closing   bool
	conns     map[*trackedServerConn]struct{}
	connWG    sync.WaitGroup
	closeOnce sync.Once

	watchStop chan struct{}
	watchDone chan struct{}
}

type trackedServerConn struct {
	net.Conn
	closeOnce sync.Once
	closeErr  error
}

func (c *trackedServerConn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}

func newServerRun(listener net.Listener) *serverRun {
	return &serverRun{
		listener:  listener,
		conns:     make(map[*trackedServerConn]struct{}),
		watchStop: make(chan struct{}),
		watchDone: make(chan struct{}),
	}
}

func (r *serverRun) watch(ctx context.Context, onCancel func()) {
	go func() {
		defer close(r.watchDone)
		select {
		case <-ctx.Done():
			onCancel()
			r.shutdown()
		case <-r.watchStop:
		}
	}()
}

func (r *serverRun) stopWatching() {
	close(r.watchStop)
	<-r.watchDone
}

func (r *serverRun) track(conn net.Conn) (*trackedServerConn, bool) {
	tracked := &trackedServerConn{Conn: conn}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return nil, false
	}
	r.conns[tracked] = struct{}{}
	r.connWG.Add(1)
	return tracked, true
}

func (r *serverRun) untrack(conn *trackedServerConn) {
	r.mu.Lock()
	delete(r.conns, conn)
	r.mu.Unlock()
	r.connWG.Done()
}

func (r *serverRun) shutdown() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		connections := make([]*trackedServerConn, 0, len(r.conns))
		for tracked := range r.conns {
			connections = append(connections, tracked)
		}
		r.mu.Unlock()

		// Close may wake code that needs the lifecycle mutex, so never invoke
		// user-supplied Listener or Conn implementations while holding it.
		_ = r.listener.Close()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
}

func (r *serverRun) wait() {
	r.connWG.Wait()
}

func (s *Server) acquireHandshake() bool {
	if s.handshakes == nil {
		return true
	}
	select {
	case s.handshakes <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseHandshake() {
	if s.handshakes != nil {
		<-s.handshakes
	}
}

func (s *Server) debugf(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		// debug logs only emit if enabled on the slogger (verbose is enabled)
		s.config.Logger.Debug(fmt.Sprintf(f, args...))
	}
}

func (s *Server) infof(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		s.config.Logger.Info(fmt.Sprintf(f, args...))
	}
}

func (s *Server) errorf(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		s.config.Logger.Error(fmt.Sprintf(f, args...))
	}
}
