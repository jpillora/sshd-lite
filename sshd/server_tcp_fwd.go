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

// handleTCPIPForward handles reverse port forwarding requests (global request)
func (h *TCPForwardingHandler) handleTCPIPForward(conn ssh.Conn, req *Request) error {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal tcpip-forward request: %w", err)
	}

	// Bind to the requested address
	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Reverse forwarding request for %s", bindAddr)

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", bindAddr, err)
	}

	// Store the listener
	h.listenersMu.Lock()
	h.listeners[bindAddr] = listener
	h.listenersMu.Unlock()

	// Get the actual port if 0 was requested
	actualPort := uint32(listener.Addr().(*net.TCPAddr).Port)

	// Reply with the actual port (handler is responsible for reply on success)
	if req.WantReply {
		if err := req.Reply(true, []byte{
			byte(actualPort >> 24),
			byte(actualPort >> 16),
			byte(actualPort >> 8),
			byte(actualPort >> 0),
		}); err != nil {
			h.server.errorf("Failed to reply to TCP forwarding request: %s", err)
		}
	}

	h.server.debugf("Reverse forwarding established on %s (actual port: %d)", bindAddr, actualPort)

	// Start accepting connections
	go h.acceptReverseConnections(listener, conn, payload.Host, actualPort)
	return nil
}

// handleCancelTCPIPForward handles cancellation of reverse port forwarding (global request)
func (h *TCPForwardingHandler) handleCancelTCPIPForward(conn ssh.Conn, req *Request) error {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal cancel-tcpip-forward request: %w", err)
	}

	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Cancel reverse forwarding request for %s", bindAddr)

	h.listenersMu.Lock()
	listener, exists := h.listeners[bindAddr]
	if exists {
		delete(h.listeners, bindAddr)
	}
	h.listenersMu.Unlock()

	if !exists {
		return fmt.Errorf("no reverse forwarding found for %s", bindAddr)
	}

	listener.Close()
	h.server.debugf("Cancelled reverse forwarding for %s", bindAddr)

	// Reply success (handler is responsible for reply on success)
	if req.WantReply {
		if err := req.Reply(true, nil); err != nil {
			h.server.errorf("Failed to reply to TCP forwarding cancel request: %s", err)
		}
	}
	return nil
}

// HandleDirectTCPIP handles direct TCP/IP forwarding (local forwarding) - channel handler
func (h *TCPForwardingHandler) HandleDirectTCPIP(newChannel ssh.NewChannel) error {
	var payload struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}

	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		if rejectErr := newChannel.Reject(ssh.ConnectionFailed, "Invalid payload"); rejectErr != nil {
			h.server.errorf("Failed to reject channel with invalid payload: %s", rejectErr)
		}
		return fmt.Errorf("failed to unmarshal direct-tcpip request: %w", err)
	}

	destAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	h.server.debugf("Direct TCP forwarding request to %s from %s:%d", destAddr, payload.OriginHost, payload.OriginPort)

	// Connect to the target
	tcpConn, err := net.Dial("tcp", destAddr)
	if err != nil {
		if rejectErr := newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("Failed to connect to %s", destAddr)); rejectErr != nil {
			h.server.errorf("Failed to reject channel for connection to %s: %s", destAddr, rejectErr)
		}
		return fmt.Errorf("failed to connect to %s: %w", destAddr, err)
	}

	// Accept the channel
	channel, reqs, err := newChannel.Accept()
	if err != nil {
		tcpConn.Close()
		return fmt.Errorf("failed to accept direct-tcpip channel: %w", err)
	}

	// Discard any requests on this channel
	go ssh.DiscardRequests(reqs)

	h.server.debugf("Direct TCP forwarding established to %s", destAddr)

	// Pipe data between the SSH channel and TCP connection
	go func() {
		defer channel.Close()
		defer tcpConn.Close()
		h.pipeConnections(channel, tcpConn)
	}()

	return nil
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

// pipeConnections pipes data between two connections
func (h *TCPForwardingHandler) pipeConnections(conn1 io.ReadWriteCloser, conn2 io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from conn1 to conn2
	go func() {
		defer wg.Done()
		if _, err := io.Copy(conn2, conn1); err != nil {
			h.server.debugf("Error copying from conn1 to conn2: %s", err)
		}
		if closer, ok := conn2.(interface{ CloseWrite() error }); ok {
			if err := closer.CloseWrite(); err != nil {
				h.server.debugf("Error closing write on conn2: %s", err)
			}
		}
	}()

	// Copy from conn2 to conn1
	go func() {
		defer wg.Done()
		if _, err := io.Copy(conn1, conn2); err != nil {
			h.server.debugf("Error copying from conn2 to conn1: %s", err)
		}
		if closer, ok := conn1.(interface{ CloseWrite() error }); ok {
			if err := closer.CloseWrite(); err != nil {
				h.server.debugf("Error closing write on conn1: %s", err)
			}
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
