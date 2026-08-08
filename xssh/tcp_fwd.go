package xssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// TCPForwardingHandler manages TCP forwarding functionality.
// It can be used with any xssh.Conn to enable TCP forwarding.
type TCPForwardingHandler struct {
	mu          sync.Mutex
	listeners   map[string]*tcpForwardListener
	connections map[*reverseTCPConnection]struct{}
	closed      bool
}

type tcpForwardListener struct {
	listener net.Listener
	bindAddr string
	host     string
	port     uint32
}

type reverseTCPConnection struct {
	conn net.Conn
}

// NewTCPForwardingHandler creates a new TCP forwarding handler.
func NewTCPForwardingHandler() *TCPForwardingHandler {
	return &TCPForwardingHandler{
		listeners:   make(map[string]*tcpForwardListener),
		connections: make(map[*reverseTCPConnection]struct{}),
	}
}

// HandleTCPIPForward handles reverse port forwarding requests (global request).
// Register this as the handler for "tcpip-forward" global requests.
func (h *TCPForwardingHandler) HandleTCPIPForward(conn Conn, req *Request) error {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal tcpip-forward request: %w", err)
	}

	// Bind to the requested address
	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	conn.debugf("Reverse forwarding request for %s", bindAddr)

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", bindAddr, err)
	}

	// Get the actual port if 0 was requested
	actualPort := uint32(listener.Addr().(*net.TCPAddr).Port)
	actualBindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", actualPort))
	forwardListener := &tcpForwardListener{
		listener: listener,
		bindAddr: actualBindAddr,
		host:     payload.Host,
		port:     actualPort,
	}
	if err := h.registerListener(forwardListener); err != nil {
		return err
	}

	// Reply with the actual port (handler is responsible for reply on success)
	if req.WantReply {
		var replyPayload []byte
		if payload.Port == 0 {
			replyPayload = ssh.Marshal(&struct{ Port uint32 }{Port: actualPort})
		}
		if err := req.Reply(true, replyPayload); err != nil {
			h.removeListener(actualBindAddr, forwardListener)
			closeErr := closeListener(listener)
			return errors.Join(fmt.Errorf("failed to reply to TCP forwarding request: %w", err), closeErr)
		}
	}

	conn.debugf("Reverse forwarding established on %s", actualBindAddr)

	// Start accepting connections
	go h.acceptReverseConnections(forwardListener, conn)
	return nil
}

// HandleCancelTCPIPForward handles cancellation of reverse port forwarding (global request).
// Register this as the handler for "cancel-tcpip-forward" global requests.
func (h *TCPForwardingHandler) HandleCancelTCPIPForward(conn Conn, req *Request) error {
	var payload struct {
		Host string
		Port uint32
	}

	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal cancel-tcpip-forward request: %w", err)
	}

	bindAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	conn.debugf("Cancel reverse forwarding request for %s", bindAddr)

	listener, exists := h.takeListener(bindAddr)

	if !exists {
		return fmt.Errorf("no reverse forwarding found for %s", bindAddr)
	}

	if err := closeListener(listener.listener); err != nil {
		return fmt.Errorf("failed to close reverse forwarding listener for %s: %w", bindAddr, err)
	}
	conn.debugf("Cancelled reverse forwarding for %s", bindAddr)

	// Reply success (handler is responsible for reply on success)
	if req.WantReply {
		if err := req.Reply(true, nil); err != nil {
			return fmt.Errorf("failed to reply to TCP forwarding cancel request: %w", err)
		}
	}
	return nil
}

// HandleDirectTCPIP handles direct TCP/IP forwarding (local forwarding) - channel handler.
// Register this as the handler for "direct-tcpip" channels.
func (h *TCPForwardingHandler) HandleDirectTCPIP(conn Conn, newChannel ssh.NewChannel) error {
	var payload struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}

	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		if rejectErr := newChannel.Reject(ssh.ConnectionFailed, "Invalid payload"); rejectErr != nil {
			conn.errorf("Failed to reject channel with invalid payload: %s", rejectErr)
		}
		return fmt.Errorf("failed to unmarshal direct-tcpip request: %w", err)
	}

	destAddr := net.JoinHostPort(payload.Host, fmt.Sprintf("%d", payload.Port))
	conn.debugf("Direct TCP forwarding request to %s from %s:%d", destAddr, payload.OriginHost, payload.OriginPort)

	// Connect to the target
	tcpConn, err := net.Dial("tcp", destAddr)
	if err != nil {
		if rejectErr := newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("Failed to connect to %s", destAddr)); rejectErr != nil {
			conn.errorf("Failed to reject channel for connection to %s: %s", destAddr, rejectErr)
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

	conn.debugf("Direct TCP forwarding established to %s", destAddr)

	// Pipe data between the SSH channel and TCP connection
	go func() {
		defer channel.Close()
		defer tcpConn.Close()
		pipeConnections(conn, channel, tcpConn)
	}()

	return nil
}

// acceptReverseConnections accepts incoming connections for reverse forwarding.
func (h *TCPForwardingHandler) acceptReverseConnections(forwardListener *tcpForwardListener, conn Conn) {
	listener := forwardListener.listener
	defer func() {
		h.removeListener(forwardListener.bindAddr, forwardListener)
		if err := closeListener(listener); err != nil {
			conn.errorf("Failed to close reverse forwarding listener for %s: %s", forwardListener.bindAddr, err)
		}
	}()

	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				conn.errorf("Failed to accept connection for reverse forwarding on %s: %v", forwardListener.bindAddr, err)
			}
			return
		}

		trackedConn, ok := h.trackConnection(tcpConn)
		if !ok {
			if err := tcpConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				conn.errorf("Failed to close reverse forwarding connection during shutdown: %s", err)
			}
			return
		}
		conn.debugf("Accepted reverse forwarding connection from %s", tcpConn.RemoteAddr())
		go h.handleReverseConnection(trackedConn, conn, forwardListener.host, forwardListener.port)
	}
}

// handleReverseConnection handles a single reverse forwarding connection
func (h *TCPForwardingHandler) handleReverseConnection(trackedConn *reverseTCPConnection, conn Conn, host string, port uint32) {
	tcpConn := trackedConn.conn
	defer func() {
		h.untrackConnection(trackedConn)
		if err := tcpConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			conn.debugf("Failed to close reverse forwarding connection: %s", err)
		}
	}()

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
	channel, reqs, err := conn.OpenChannel("forwarded-tcpip", payloadBytes)
	if err != nil {
		conn.debugf("Failed to open forwarded-tcpip channel: %v", err)
		return
	}
	defer channel.Close()

	// Discard any requests on this channel
	go ssh.DiscardRequests(reqs)

	// Pipe data between the TCP connection and SSH channel
	conn.debugf("Piping data for reverse forwarding connection")
	pipeConnections(conn, tcpConn, channel)
}

// Close closes all reverse-forward listeners and accepted TCP connections.
func (h *TCPForwardingHandler) Close() {
	_ = h.closeAll()
}

func (h *TCPForwardingHandler) registerListener(listener *tcpForwardListener) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errors.Join(errors.New("tcp forwarding handler is closed"), closeListener(listener.listener))
	}
	if _, exists := h.listeners[listener.bindAddr]; exists {
		h.mu.Unlock()
		return errors.Join(fmt.Errorf("reverse forwarding already exists for %s", listener.bindAddr), closeListener(listener.listener))
	}
	h.listeners[listener.bindAddr] = listener
	h.mu.Unlock()
	return nil
}

func (h *TCPForwardingHandler) takeListener(bindAddr string) (*tcpForwardListener, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	listener, exists := h.listeners[bindAddr]
	if exists {
		delete(h.listeners, bindAddr)
	}
	return listener, exists
}

// removeListener deletes only the supplied registration. This prevents a late
// accept-loop exit from deleting a newer listener registered for the same key.
func (h *TCPForwardingHandler) removeListener(bindAddr string, listener *tcpForwardListener) {
	h.mu.Lock()
	if h.listeners[bindAddr] == listener {
		delete(h.listeners, bindAddr)
	}
	h.mu.Unlock()
}

func (h *TCPForwardingHandler) trackConnection(conn net.Conn) (*reverseTCPConnection, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	trackedConn := &reverseTCPConnection{conn: conn}
	h.connections[trackedConn] = struct{}{}
	return trackedConn, true
}

func (h *TCPForwardingHandler) untrackConnection(conn *reverseTCPConnection) {
	h.mu.Lock()
	delete(h.connections, conn)
	h.mu.Unlock()
}

func (h *TCPForwardingHandler) closeAll() error {
	h.mu.Lock()
	h.closed = true
	listeners := h.listeners
	connections := h.connections
	h.listeners = make(map[string]*tcpForwardListener)
	h.connections = make(map[*reverseTCPConnection]struct{})
	h.mu.Unlock()

	var errs []error
	for _, listener := range listeners {
		if err := closeListener(listener.listener); err != nil {
			errs = append(errs, fmt.Errorf("close reverse forwarding listener %s: %w", listener.bindAddr, err))
		}
	}
	for conn := range connections {
		if err := conn.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("close reverse forwarding connection: %w", err))
		}
	}
	return errors.Join(errs...)
}

func closeListener(listener net.Listener) error {
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// pipeConnections pipes data between two connections
func pipeConnections(conn Conn, conn1 io.ReadWriteCloser, conn2 io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from conn1 to conn2
	go func() {
		defer wg.Done()
		if _, err := io.Copy(conn2, conn1); err != nil {
			conn.debugf("Error copying from conn1 to conn2: %s", err)
		}
		if closer, ok := conn2.(interface{ CloseWrite() error }); ok {
			if err := closer.CloseWrite(); err != nil {
				conn.debugf("Error closing write on conn2: %s", err)
			}
		}
	}()

	// Copy from conn2 to conn1
	go func() {
		defer wg.Done()
		if _, err := io.Copy(conn1, conn2); err != nil {
			conn.debugf("Error copying from conn2 to conn1: %s", err)
		}
		if closer, ok := conn1.(interface{ CloseWrite() error }); ok {
			if err := closer.CloseWrite(); err != nil {
				conn.debugf("Error closing write on conn1: %s", err)
			}
		}
	}()

	wg.Wait()
	conn.debugf("TCP forwarding connection closed")
}
