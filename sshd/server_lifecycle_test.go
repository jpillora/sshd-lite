package sshd

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

func TestStartWithContextAlreadyCancelled(t *testing.T) {
	server := newLifecycleTestServer(t, Config{})
	listener := &controlledListener{acceptErr: errors.New("Accept must not be called")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.StartWithContext(ctx, listener); err != nil {
		t.Fatalf("StartWithContext returned an error for a pre-cancelled context: %v", err)
	}
	if got := listener.accepts.Load(); got != 0 {
		t.Fatalf("Accept called %d times, want 0", got)
	}
	if got := listener.closes.Load(); got != 1 {
		t.Fatalf("Close called %d times, want 1", got)
	}
}

func TestStartWithContextCancellationClosesActiveConnectionAndWaits(t *testing.T) {
	handlerContextDone := make(chan struct{})
	channelStarted := make(chan struct{})
	channelCleaned := make(chan struct{})
	server := newLifecycleTestServer(t, Config{
		ConnectionHandler: func(ctx context.Context, _ *ssh.ServerConn) {
			<-ctx.Done()
			close(handlerContextDone)
		},
		ChannelHandlers: map[string]ChannelHandler{
			"lifecycle-test": func(conn xssh.Conn, ch ssh.NewChannel) error {
				channel, _, err := ch.Accept()
				if err != nil {
					return err
				}
				defer channel.Close()
				close(channelStarted)
				_ = conn.Wait()
				close(channelCleaned)
				return nil
			},
		},
	})
	listener := listenLifecycleTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.StartWithContext(ctx, listener) }()

	client := dialLifecycleTest(t, listener.Addr().String())
	defer client.Close()
	channel, _, err := client.OpenChannel("lifecycle-test", nil)
	if err != nil {
		cancel()
		t.Fatalf("open lifecycle channel: %v", err)
	}
	defer channel.Close()
	awaitLifecycleSignal(t, channelStarted, "custom channel start")

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StartWithContext returned an error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartWithContext did not finish after cancellation")
	}
	select {
	case <-channelCleaned:
	default:
		t.Fatal("StartWithContext returned before the active channel handler finished")
	}
	awaitLifecycleSignal(t, handlerContextDone, "ConnectionHandler context cancellation")
	if _, _, err := client.SendRequest("after-shutdown", true, nil); err == nil {
		t.Fatal("authenticated client remained connected after server shutdown")
	}
}

func TestStartWithContextCancellationClosesIdleHandshake(t *testing.T) {
	server := newLifecycleTestServer(t, Config{HandshakeTimeout: time.Minute})
	listener := &countingAcceptListener{
		Listener: listenLifecycleTest(t),
		accepted: make(chan *countingConn, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.StartWithContext(ctx, listener) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("dial idle handshake: %v", err)
	}
	defer conn.Close()
	readSSHBanner(t, bufio.NewReader(conn))
	accepted := <-listener.accepted
	cancel()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var one [1]byte
	if _, err := conn.Read(one[:]); err == nil {
		t.Fatal("idle handshake remained open after cancellation")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StartWithContext returned an error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartWithContext did not wait for the idle handshake to finish")
	}
	if got := accepted.closes.Load(); got != 1 {
		t.Fatalf("accepted transport closed %d times, want exactly once", got)
	}
}

func TestStartWithContextDoesNotWaitForConnectionHandler(t *testing.T) {
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := newLifecycleTestServer(t, Config{
		ConnectionHandler: func(context.Context, *ssh.ServerConn) {
			close(handlerStarted)
			<-releaseHandler
		},
	})
	listener := listenLifecycleTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.StartWithContext(ctx, listener) }()

	client := dialLifecycleTest(t, listener.Addr().String())
	defer client.Close()
	awaitLifecycleSignal(t, handlerStarted, "ConnectionHandler start")
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StartWithContext returned an error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(releaseHandler)
		t.Fatal("an uncooperative ConnectionHandler blocked server shutdown")
	}
	close(releaseHandler)
}

func TestStartWithContextPreservesAcceptErrorAndStopsWatcher(t *testing.T) {
	acceptFailure := errors.New("controlled accept failure")
	listener := &controlledListener{acceptErr: acceptFailure}
	server := newLifecycleTestServer(t, Config{})
	ctx, cancel := context.WithCancel(context.Background())

	err := server.StartWithContext(ctx, listener)
	if !errors.Is(err, acceptFailure) {
		t.Fatalf("StartWithContext error = %v, want wrapped Accept error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "accept failed") {
		t.Fatalf("StartWithContext error = %v, want meaningful Accept context", err)
	}
	if got := listener.closes.Load(); got != 1 {
		t.Fatalf("Close called %d times before cancellation, want 1", got)
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	if got := listener.closes.Load(); got != 1 {
		t.Fatalf("late cancellation invoked stopped watcher: Close called %d times", got)
	}
}

type controlledListener struct {
	acceptErr error
	accepts   atomic.Int32
	closes    atomic.Int32
}

func (l *controlledListener) Accept() (net.Conn, error) {
	l.accepts.Add(1)
	return nil, l.acceptErr
}

func (l *controlledListener) Close() error {
	l.closes.Add(1)
	return nil
}

func (l *controlledListener) Addr() net.Addr { return lifecycleAddr("controlled") }

type countingAcceptListener struct {
	net.Listener
	accepted chan *countingConn
}

func (l *countingAcceptListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	counted := &countingConn{Conn: conn}
	l.accepted <- counted
	return counted, nil
}

type countingConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *countingConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

type lifecycleAddr string

func (a lifecycleAddr) Network() string { return string(a) }
func (a lifecycleAddr) String() string  { return string(a) }

func newLifecycleTestServer(t *testing.T, overrides Config) *Server {
	t.Helper()
	overrides.AuthType = "none"
	overrides.KeySeed = "server-lifecycle-test"
	overrides.KeySeedEC = true
	overrides.LogQuiet = true
	server, err := NewServer(overrides)
	if err != nil {
		t.Fatalf("create lifecycle server: %v", err)
	}
	return server
}

func listenLifecycleTest(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener
}

func dialLifecycleTest(t *testing.T, addr string) *ssh.Client {
	t.Helper()
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial SSH server: %v", err)
	}
	return client
}

func awaitLifecycleSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
