package sshd

import (
	"context"
	"io"
	"net"

	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

// HandleConn handles a new TCP connection
func (s *Server) HandleConn(tcpConn net.Conn) {
	// Before use, a handshake must be performed on the incoming net.Conn.
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, s.sshConfig)
	if err != nil {
		if err != io.EOF {
			s.errorf("Failed to handshake (%s)", err)
		}
		return
	}
	s.debugf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

	// Call connection handler if configured
	if h := s.config.ConnectionHandler; h != nil {
		ctx, cancel := context.WithCancel(context.Background())
		go h(ctx, sshConn)
		go func() {
			sshConn.Wait()
			cancel()
		}()
	}

	// Wrap the connection in an xssh.Conn and serve
	conn := xssh.NewConn(sshConn, chans, reqs, s.xsshConfig)
	conn.Serve()
}
