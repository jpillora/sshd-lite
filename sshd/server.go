package sshd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/jpillora/jplog"
	"golang.org/x/crypto/ssh"
)

// Server is a simple SSH Daemon
type Server struct {
	config                 Config
	sshConfig              *ssh.ServerConfig
	globalRequestHandlers  map[string]GlobalRequestHandler
	channelHandlers        map[string]ChannelHandler
	sessionRequestHandlers map[string]SessionRequestHandler
	subsystemHandlers      map[string]SubsystemHandler
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
	// initialize handler maps
	s.globalRequestHandlers = map[string]GlobalRequestHandler{}
	s.channelHandlers = map[string]ChannelHandler{}
	s.sessionRequestHandlers = map[string]SessionRequestHandler{}
	s.subsystemHandlers = map[string]SubsystemHandler{}
	// register built-in session handlers
	s.sessionRequestHandlers["pty-req"] = handlePtyReq
	s.sessionRequestHandlers["window-change"] = handleWindowChange
	s.sessionRequestHandlers["env"] = handleEnv
	s.sessionRequestHandlers["shell"] = handleShell
	s.sessionRequestHandlers["exec"] = handleExec
	// register built-in channel handler
	s.channelHandlers["session"] = s.handleSessionChannel
	// register TCP forwarding handlers if enabled
	if c.TCPForwarding {
		tfh := NewTCPForwardingHandler(s)
		s.globalRequestHandlers["tcpip-forward"] = tfh.handleTCPIPForward
		s.globalRequestHandlers["cancel-tcpip-forward"] = tfh.handleCancelTCPIPForward
		s.channelHandlers["direct-tcpip"] = tfh.HandleDirectTCPIP
		s.infof("TCP forwarding enabled")
	}
	// register SFTP subsystem if enabled
	if c.SFTP {
		s.subsystemHandlers["sftp"] = NewSFTPHandler(s)
		s.infof("SFTP enabled")
	}
	// merge custom handlers from config (fail on clash with built-in)
	for name, h := range c.GlobalRequestHandlers {
		if _, exists := s.globalRequestHandlers[name]; exists {
			return nil, fmt.Errorf("global request handler %q already registered", name)
		}
		s.globalRequestHandlers[name] = h
	}
	for name, h := range c.ChannelHandlers {
		if _, exists := s.channelHandlers[name]; exists {
			return nil, fmt.Errorf("channel handler %q already registered", name)
		}
		s.channelHandlers[name] = h
	}
	for name, h := range c.SessionRequestHandlers {
		if _, exists := s.sessionRequestHandlers[name]; exists {
			return nil, fmt.Errorf("session request handler %q already registered", name)
		}
		s.sessionRequestHandlers[name] = h
	}
	for name, h := range c.SubsystemHandlers {
		if _, exists := s.subsystemHandlers[name]; exists {
			return nil, fmt.Errorf("subsystem handler %q already registered", name)
		}
		s.subsystemHandlers[name] = h
	}
	return s, nil
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
			// Check if the error is due to listener being closed
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil // Expected error when stopping
			}
			s.errorf("Failed to accept incoming connection (%s)", err)
			continue
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
