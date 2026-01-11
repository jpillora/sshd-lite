package sshd

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"

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

	// Handle global requests
	go s.handleGlobalRequests(reqs, sshConn)

	// Accept all channels
	go s.handleChannels(chans)
}

// handleGlobalRequests dispatches global requests to registered handlers
func (s *Server) handleGlobalRequests(reqs <-chan *ssh.Request, conn ssh.Conn) {
	for sshReq := range reqs {
		s.debugf("Global request: %s", sshReq.Type)
		handler, ok := s.globalRequestHandlers[sshReq.Type]
		if !ok {
			s.debugf("No handler for global request: %s", sshReq.Type)
			if sshReq.WantReply {
				if err := sshReq.Reply(false, nil); err != nil {
					s.errorf("Failed to reply to global request %q: %s", sshReq.Type, err)
				}
			}
			continue
		}
		req := Wrap(sshReq)
		err := handler(conn, req)
		if err != nil {
			s.errorf("Global request %q failed: %s", req.Type, err)
		}
		if req.WantReply && !req.Replied() {
			if replyErr := req.Reply(err == nil, nil); replyErr != nil {
				s.errorf("Failed to reply to global request %q: %s", req.Type, replyErr)
			}
		}
	}
}

// handleChannels dispatches incoming channels to registered handlers
func (s *Server) handleChannels(chans <-chan ssh.NewChannel) {
	for newChannel := range chans {
		go s.handleChannel(newChannel)
	}
}

// handleChannel dispatches a single channel to its handler
func (s *Server) handleChannel(newChannel ssh.NewChannel) {
	channelType := newChannel.ChannelType()
	s.debugf("Channel request '%s'", channelType)

	handler, ok := s.channelHandlers[channelType]
	if !ok {
		s.debugf("Unknown channel type: %s", channelType)
		if err := newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType)); err != nil {
			s.errorf("Failed to reject unknown channel type %q: %s", channelType, err)
		}
		return
	}

	// dispatch to handler with error wrapping
	if err := handler(newChannel); err != nil {
		s.errorf("Channel %q failed: %s", channelType, err)
		// handler is responsible for rejecting the channel with appropriate reason
	}
}

// handleSessionChannel handles the "session" channel type
func (s *Server) handleSessionChannel(newChannel ssh.NewChannel) error {
	if d := newChannel.ExtraData(); len(d) > 0 {
		s.debugf("Channel data: '%s' %x", d, d)
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		return fmt.Errorf("could not accept channel: %w", err)
	}
	s.debugf("Channel accepted")

	// create session and handle requests
	sess := &Session{
		server:  s,
		Channel: channel,
		Env:     os.Environ(),
		Resizes: make(chan []byte, 10),
	}
	go s.handleSessionRequests(sess, requests)
	return nil
}

// handleSessionRequests dispatches session requests to registered handlers
func (s *Server) handleSessionRequests(sess *Session, requests <-chan *ssh.Request) {
	defer close(sess.Resizes)

	// start keep alive loop
	if ka := s.config.KeepAlive; ka > 0 {
		ticking := make(chan bool, 1)
		interval := time.Duration(ka) * time.Second
		go s.keepAlive(sess.Channel, interval, ticking)
		defer close(ticking)
	}

	// process requests
	for sshReq := range requests {
		s.debugf("Session request: %s", sshReq.Type)
		req := Wrap(sshReq)

		// special case: subsystem requests are dispatched differently
		if sshReq.Type == "subsystem" {
			ok := s.handleSubsystemRequest(sess, req)
			if req.WantReply && !req.Replied() {
				if err := req.Reply(ok, nil); err != nil {
					s.errorf("Failed to reply to subsystem request %q: %s", sshReq.Type, err)
				}
			}
			if !ok {
				sess.Channel.Close()
			}
			continue
		}

		// look up handler
		handler, ok := s.sessionRequestHandlers[sshReq.Type]
		if !ok {
			s.debugf("Unknown session request: %s (reply: %v, data: %x)", sshReq.Type, sshReq.WantReply, sshReq.Payload)
			if req.WantReply && !req.Replied() {
				if err := req.Reply(false, nil); err != nil {
					s.errorf("Failed to reply to unknown session request %q: %s", sshReq.Type, err)
				}
			}
			continue
		}

		// dispatch to handler
		err := handler(sess, req)
		if err != nil {
			s.errorf("Session request %q failed: %s", req.Type, err)
		}
		// auto-reply if handler didn't call Reply
		if req.WantReply && !req.Replied() {
			if replyErr := req.Reply(err == nil, nil); replyErr != nil {
				s.errorf("Failed to reply to session request %q: %s", req.Type, replyErr)
			}
		}
	}
	s.debugf("Closing handler for session requests")
}

// handleSubsystemRequest handles 'subsystem' session requests
func (s *Server) handleSubsystemRequest(sess *Session, req *Request) bool {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// subsystem name is a string encoded as: [uint32 length][string name]
	if len(req.Payload) < 4 {
		s.debugf("Malformed subsystem request payload")
		return false
	}
	length := binary.BigEndian.Uint32(req.Payload)
	if uint32(len(req.Payload)-4) != length {
		s.debugf("Subsystem name length mismatch in payload")
		return false
	}
	subsystem := string(req.Payload[4:])

	handler, ok := s.subsystemHandlers[subsystem]
	if !ok {
		s.debugf("Unsupported subsystem requested: %q", subsystem)
		return false
	}

	// dispatch to handler with error wrapping
	if err := handler(sess.Channel, req); err != nil {
		s.errorf("Subsystem %q failed: %s", subsystem, err)
		return false
	}
	return true
}

// keepAlive sends periodic ping requests to keep the connection alive
func (s *Server) keepAlive(channel ssh.Channel, interval time.Duration, ticking <-chan bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err := channel.SendRequest("ping", false, nil)
			if err != nil {
				s.debugf("Failed to send keep alive ping: %s", err)
			}
			s.debugf("Sent keep alive ping")
		case <-ticking:
			return
		}
	}
}
