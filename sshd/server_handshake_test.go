package sshd

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestHandshakeDefaultsProtectProgrammaticServers(t *testing.T) {
	s := newHandshakeTestServer(t, Config{})
	if got := s.Config().HandshakeTimeout; got != DefaultHandshakeTimeout {
		t.Fatalf("HandshakeTimeout = %s, want %s", got, DefaultHandshakeTimeout)
	}
	if got := s.Config().MaxPendingHandshakes; got != DefaultMaxPendingHandshakes {
		t.Fatalf("MaxPendingHandshakes = %d, want %d", got, DefaultMaxPendingHandshakes)
	}
}

func TestIdleHandshakeTimesOut(t *testing.T) {
	s := newHandshakeTestServer(t, Config{
		HandshakeTimeout:     25 * time.Millisecond,
		MaxPendingHandshakes: 1,
	})
	serverConn, clientConn := tcpConnPair(t)
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		s.HandleConn(serverConn)
		close(done)
	}()

	reader := bufio.NewReader(clientConn)
	readSSHBanner(t, reader)
	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("idle connection remained open after the handshake timeout")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HandleConn did not return after the handshake timeout")
	}
}

func TestStartRejectsHandshakeOverAdmissionLimit(t *testing.T) {
	s := newHandshakeTestServer(t, Config{
		HandshakeTimeout:     time.Minute,
		MaxPendingHandshakes: 1,
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.StartWithContext(ctx, listener)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	first, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial first connection: %v", err)
	}
	defer first.Close()
	readSSHBanner(t, bufio.NewReader(first))

	second, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial second connection: %v", err)
	}
	defer second.Close()
	if err := second.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set second connection read deadline: %v", err)
	}
	var buf [1]byte
	if _, err := second.Read(buf[:]); err == nil {
		t.Fatal("connection above the handshake admission limit was not closed")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("connection above the handshake admission limit was not rejected promptly: %v", err)
	}
}

func TestHandshakeAdmissionSlotRecovers(t *testing.T) {
	s := newHandshakeTestServer(t, Config{
		HandshakeTimeout:     time.Minute,
		MaxPendingHandshakes: 1,
	})

	serverConn, clientConn := tcpConnPair(t)
	firstDone := make(chan struct{})
	go func() {
		s.HandleConn(serverConn)
		close(firstDone)
	}()
	readSSHBanner(t, bufio.NewReader(clientConn))
	clientConn.Close()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first handshake did not release its admission slot")
	}

	serverConn, clientConn = tcpConnPair(t)
	defer clientConn.Close()
	secondDone := make(chan struct{})
	go func() {
		s.HandleConn(serverConn)
		close(secondDone)
	}()
	readSSHBanner(t, bufio.NewReader(clientConn))
	clientConn.Close()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second handshake did not finish")
	}
}

func TestSuccessfulHandshakeClearsDeadline(t *testing.T) {
	s := newHandshakeTestServer(t, Config{
		HandshakeTimeout:     time.Second,
		MaxPendingHandshakes: 1,
	})
	serverConn, clientConn := tcpConnPair(t)
	observed := &deadlineObservingConn{
		Conn:      serverConn,
		deadlines: make(chan time.Time, 2),
	}
	serverDone := make(chan struct{})
	go func() {
		s.HandleConn(observed)
		close(serverDone)
	}()

	clientConnSSH, chans, reqs, err := ssh.NewClientConn(clientConn, "test", &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatalf("complete SSH handshake: %v", err)
	}
	client := ssh.NewClient(clientConnSSH, chans, reqs)
	defer client.Close()

	setDeadline := nextObservedDeadline(t, observed.deadlines)
	if setDeadline.IsZero() {
		t.Fatal("handshake deadline was not set")
	}
	clearedDeadline := nextObservedDeadline(t, observed.deadlines)
	if !clearedDeadline.IsZero() {
		t.Fatalf("handshake deadline was not cleared: %s", clearedDeadline)
	}

	// The established connection must not retain the sole handshake slot.
	nextServerConn, nextClientConn := tcpConnPair(t)
	nextDone := make(chan struct{})
	go func() {
		s.HandleConn(nextServerConn)
		close(nextDone)
	}()
	readSSHBanner(t, bufio.NewReader(nextClientConn))
	nextClientConn.Close()
	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("second handshake did not finish while first connection was established")
	}

	accepted, _, err := client.SendRequest("test-after-handshake", true, nil)
	if err != nil {
		t.Fatalf("established session failed after clearing handshake deadline: %v", err)
	}
	if accepted {
		t.Fatal("unknown test request was unexpectedly accepted")
	}

	client.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("HandleConn did not return after the established client closed")
	}
}

type deadlineObservingConn struct {
	net.Conn
	deadlines chan time.Time
}

func (c *deadlineObservingConn) SetDeadline(deadline time.Time) error {
	c.deadlines <- deadline
	return c.Conn.SetDeadline(deadline)
}

func nextObservedDeadline(t *testing.T, deadlines <-chan time.Time) time.Time {
	t.Helper()
	select {
	case deadline := <-deadlines:
		return deadline
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection deadline change")
		return time.Time{}
	}
}

func newHandshakeTestServer(t *testing.T, limits Config) *Server {
	t.Helper()
	limits.AuthType = "none"
	limits.KeySeed = "handshake-test-server"
	limits.KeySeedEC = true
	limits.LogQuiet = true
	s, err := NewServer(limits)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return s
}

func tcpConnPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for connection pair: %v", err)
	}
	defer listener.Close()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial connection pair: %v", err)
	}
	serverConn, err := listener.Accept()
	if err != nil {
		clientConn.Close()
		t.Fatalf("accept connection pair: %v", err)
	}
	return serverConn, clientConn
}

func readSSHBanner(t *testing.T, reader *bufio.Reader) {
	t.Helper()
	banner, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read SSH banner: %v", err)
	}
	if !strings.HasPrefix(banner, "SSH-") {
		t.Fatalf("unexpected SSH banner %q", banner)
	}
}
