package sshd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/jpillora/jplog"
	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

// Server is a simple SSH Daemon
type Server struct {
	config    Config
	sshConfig *ssh.ServerConfig

	// xssh configuration built from sshd config
	xsshConfig *xssh.Config
}

// NewServer creates a new Server
func NewServer(c Config) (*Server, error) {
	if l := c.Logger; l == nil && !c.LogQuiet {
		h := jplog.Handler(os.Stdout)
		if c.LogVerbose {
			h = h.Verbose()
		}
		l = slog.New(h)
		c.Logger = l
	}
	s := &Server{config: c}
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
		WorkingDirectory:       c.WorkDir,
		Shell:                  c.Shell,
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
	xc.ChannelHandlers["session"] = func(conn xssh.Conn, ch ssh.NewChannel) error {
		return conn.HandleSessionChannel(ch)
	}

	if c.TCPForwarding {
		s.infof("TCP forwarding enabled")
	}
	if c.SFTP {
		s.infof("SFTP enabled")
	}
	// Merge custom handlers from config (fail on clash with built-in)
	for name, h := range c.GlobalRequestHandlers {
		if _, exists := xc.GlobalRequestHandlers[name]; exists {
			return nil, fmt.Errorf("global request handler %q already registered", name)
		}
		xc.GlobalRequestHandlers[name] = h
	}
	for name, h := range c.ChannelHandlers {
		if _, exists := xc.ChannelHandlers[name]; exists {
			return nil, fmt.Errorf("channel handler %q already registered", name)
		}
		xc.ChannelHandlers[name] = h
	}
	for name, h := range c.SessionRequestHandlers {
		if _, exists := xc.SessionRequestHandlers[name]; exists {
			return nil, fmt.Errorf("session request handler %q already registered", name)
		}
		xc.SessionRequestHandlers[name] = h
	}
	for name, h := range c.SubsystemHandlers {
		if _, exists := xc.SubsystemHandlers[name]; exists {
			return nil, fmt.Errorf("subsystem handler %q already registered", name)
		}
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
	defer l.Close()
	// Accept all connections
	s.infof("Listening on %s...", l.Addr())
	// Close listener when context is cancelled
	go func() {
		<-ctx.Done()
		s.infof("Closing server")
		l.Close()
	}()
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			return fmt.Errorf("accept failed: %w", err)
		}
		go s.HandleConn(tcpConn)
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
