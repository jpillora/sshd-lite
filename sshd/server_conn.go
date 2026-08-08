package sshd

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

// HandleConn handles a new TCP connection
func (s *Server) HandleConn(tcpConn net.Conn) {
	if !s.acquireHandshake() {
		s.debugf("Rejecting connection from %s: too many pending SSH handshakes", tcpConn.RemoteAddr())
		if err := tcpConn.Close(); err != nil {
			s.debugf("Failed to close rejected connection from %s: %s", tcpConn.RemoteAddr(), err)
		}
		return
	}
	s.handleConn(context.Background(), tcpConn)
}

func (s *Server) handleConn(ctx context.Context, tcpConn net.Conn) {
	sshConn, chans, reqs, ok := s.admittedHandshake(tcpConn)
	if !ok {
		return
	}
	defer sshConn.Close()

	s.debugf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

	// Call connection handler if configured
	if h := s.config.ConnectionHandler; h != nil {
		handlerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		// ConnectionHandler is advisory user code. Its context is cancelled
		// before this method returns, but it is not awaited so an uncooperative
		// callback cannot prevent transport shutdown.
		go h(handlerCtx, sshConn)
	}

	// Wrap the connection in an xssh.Conn and serve
	conn, err := xssh.NewConnChecked(sshConn, chans, reqs, s.xsshConfig)
	if err != nil {
		// NewServer validates this immutable internal configuration before
		// serving, so reaching this branch indicates an internal bug.
		s.errorf("Invalid internal xssh configuration: %s", err)
		return
	}
	conn.Serve()
}

func (s *Server) admittedHandshake(tcpConn net.Conn) (*ssh.ServerConn, <-chan ssh.NewChannel, <-chan *ssh.Request, bool) {
	defer s.releaseHandshake()
	return s.handshake(tcpConn)
}

func (s *Server) handshake(tcpConn net.Conn) (*ssh.ServerConn, <-chan ssh.NewChannel, <-chan *ssh.Request, bool) {
	if timeout := s.config.HandshakeTimeout; timeout > 0 {
		if err := tcpConn.SetDeadline(time.Now().Add(timeout)); err != nil {
			s.errorf("Failed to set SSH handshake deadline (%s)", err)
			tcpConn.Close()
			return nil, nil, nil, false
		}
	}

	// Before use, a handshake must be performed on the incoming net.Conn.
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, s.sshConfig)
	if err != nil {
		if err != io.EOF {
			s.errorf("Failed to handshake (%s)", err)
		}
		tcpConn.Close()
		return nil, nil, nil, false
	}
	if s.config.HandshakeTimeout > 0 {
		if err := tcpConn.SetDeadline(time.Time{}); err != nil {
			s.errorf("Failed to clear SSH handshake deadline (%s)", err)
			sshConn.Close()
			return nil, nil, nil, false
		}
	}
	return sshConn, chans, reqs, true
}
