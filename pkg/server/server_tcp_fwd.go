package sshd

import (
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// TCPForwardingHandler manages TCP forwarding functionality
type TCPForwardingHandler struct {
	server      *Server
	listeners   map[string]net.Listener
	listenersMu sync.RWMutex
}

// NewTCPForwardingHandler creates a new TCP forwarding handler
func NewTCPForwardingHandler(server *Server) *TCPForwardingHandler {
	return &TCPForwardingHandler{
		server:    server,
		listeners: make(map[string]net.Listener),
	}
}

// HandleGlobalRequest handles global SSH requests for TCP forwarding
func (h *TCPForwardingHandler) HandleGlobalRequest(req *ssh.Request, conn ssh.Conn) {
	switch req.Type {
	case "tcpip-forward":
		h.handleTCPIPForward(req, conn)
	case "cancel-tcpip-forward":
		h.handleCancelTCPIPForward(req, conn)
	default:
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// handleTCPIPForward handles reverse port forwarding requests
func (h *TCPForwardingHandler) handleTCPIPForward(req *ssh.Request, conn ssh.Conn) {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		h.server.debugf("Failed to unmarshal tcpip-forward request: %v", err)
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	// Bind to the requested address
	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Reverse forwarding request for %s", bindAddr)

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		h.server.debugf("Failed to listen on %s: %v", bindAddr, err)
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	// Store the listener
	h.listenersMu.Lock()
	h.listeners[bindAddr] = listener
	h.listenersMu.Unlock()

	// Get the actual port if 0 was requested
	actualPort := uint32(listener.Addr().(*net.TCPAddr).Port)

	// Reply with the actual port
	if req.WantReply {
		portBytes := make([]byte, 4)
		portBytes[0] = byte(actualPort >> 24)
		portBytes[1] = byte(actualPort >> 16)
		portBytes[2] = byte(actualPort >> 8)
		portBytes[3] = byte(actualPort)
		req.Reply(true, portBytes)
	}

	h.server.debugf("Reverse forwarding established on %s (actual port: %d)", bindAddr, actualPort)

	// Start accepting connections
	go h.acceptReverseConnections(listener, conn, payload.Host, actualPort)
}

// handleCancelTCPIPForward handles cancellation of reverse port forwarding
func (h *TCPForwardingHandler) handleCancelTCPIPForward(req *ssh.Request, conn ssh.Conn) {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		h.server.debugf("Failed to unmarshal cancel-tcpip-forward request: %v", err)
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Cancel reverse forwarding request for %s", bindAddr)

	h.listenersMu.Lock()
	listener, exists := h.listeners[bindAddr]
	if exists {
		delete(h.listeners, bindAddr)
	}
	h.listenersMu.Unlock()

	if exists {
		listener.Close()
		h.server.debugf("Cancelled reverse forwarding for %s", bindAddr)
		if req.WantReply {
			req.Reply(true, nil)
		}
	} else {
		h.server.debugf("No reverse forwarding found for %s", bindAddr)
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// acceptReverseConnections accepts incoming connections for reverse forwarding
func (h *TCPForwardingHandler) acceptReverseConnections(listener net.Listener, conn ssh.Conn, host string, port uint32) {
	defer listener.Close()

	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			h.server.debugf("Failed to accept connection for reverse forwarding: %v", err)
			return
		}

		h.server.debugf("Accepted reverse forwarding connection from %s", tcpConn.RemoteAddr())
		go h.handleReverseConnection(tcpConn, conn, host, port)
	}
}

// handleReverseConnection handles a single reverse forwarding connection
func (h *TCPForwardingHandler) handleReverseConnection(tcpConn net.Conn, sshConn ssh.Conn, host string, port uint32) {
	defer tcpConn.Close()

	// Open a channel to the SSH client
	remoteAddr := tcpConn.RemoteAddr().(*net.TCPAddr)
	payload := struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}{
		Host:       host,
		Port:       port,
		OriginHost: remoteAddr.IP.String(),
		OriginPort: uint32(remoteAddr.Port),
	}

	payloadBytes := ssh.Marshal(&payload)
	channel, reqs, err := sshConn.OpenChannel("forwarded-tcpip", payloadBytes)
	if err != nil {
		h.server.debugf("Failed to open forwarded-tcpip channel: %v", err)
		return
	}
	defer channel.Close()

	// Discard any requests on this channel
	go ssh.DiscardRequests(reqs)

	// Pipe data between the TCP connection and SSH channel
	h.server.debugf("Piping data for reverse forwarding connection")
	h.pipeConnections(tcpConn, channel)
}

// HandleDirectTCPIP handles direct TCP/IP forwarding (local forwarding)
func (h *TCPForwardingHandler) HandleDirectTCPIP(newChannel ssh.NewChannel) {
	var payload struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}

	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		h.server.debugf("Failed to unmarshal direct-tcpip request: %v", err)
		newChannel.Reject(ssh.ConnectionFailed, "Invalid payload")
		return
	}

	destAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Direct TCP forwarding request to %s from %s:%d", destAddr, payload.OriginHost, payload.OriginPort)

	// Connect to the target
	tcpConn, err := net.Dial("tcp", destAddr)
	if err != nil {
		h.server.debugf("Failed to connect to %s: %v", destAddr, err)
		newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("Failed to connect to %s", destAddr))
		return
	}
	defer tcpConn.Close()

	// Accept the channel
	channel, reqs, err := newChannel.Accept()
	if err != nil {
		h.server.debugf("Failed to accept direct-tcpip channel: %v", err)
		return
	}
	defer channel.Close()

	// Discard any requests on this channel
	go ssh.DiscardRequests(reqs)

	h.server.debugf("Direct TCP forwarding established to %s", destAddr)

	// Pipe data between the SSH channel and TCP connection
	h.pipeConnections(channel, tcpConn)
}

// pipeConnections pipes data between two connections
func (h *TCPForwardingHandler) pipeConnections(conn1 io.ReadWriteCloser, conn2 io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from conn1 to conn2
	go func() {
		defer wg.Done()
		io.Copy(conn2, conn1)
		if closer, ok := conn2.(interface{ CloseWrite() error }); ok {
			closer.CloseWrite()
		}
	}()

	// Copy from conn2 to conn1
	go func() {
		defer wg.Done()
		io.Copy(conn1, conn2)
		if closer, ok := conn1.(interface{ CloseWrite() error }); ok {
			closer.CloseWrite()
		}
	}()

	wg.Wait()
	h.server.debugf("TCP forwarding connection closed")
}

// Close closes all active listeners
func (h *TCPForwardingHandler) Close() {
	h.listenersMu.Lock()
	defer h.listenersMu.Unlock()

	for addr, listener := range h.listeners {
		h.server.debugf("Closing reverse forwarding listener for %s", addr)
		listener.Close()
	}
	h.listeners = make(map[string]net.Listener)
}
