package xssh

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingListener struct {
	closed atomic.Int32
}

func (l *recordingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *recordingListener) Addr() net.Addr            { return testAddr("test") }
func (l *recordingListener) Close() error {
	l.closed.Add(1)
	return nil
}

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }

func TestTCPForwardingRegistrationRejectsConcurrentDuplicates(t *testing.T) {
	handler := NewTCPForwardingHandler()
	const count = 32
	listeners := make([]*recordingListener, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range listeners {
		listeners[i] = &recordingListener{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = handler.registerListener(&tcpForwardListener{
				listener: listeners[i],
				bindAddr: "127.0.0.1:12345",
			})
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		if err == nil {
			winners++
			if got := listeners[i].closed.Load(); got != 0 {
				t.Errorf("winning listener was closed %d times before cleanup", got)
			}
			continue
		}
		if got := listeners[i].closed.Load(); got != 1 {
			t.Errorf("rejected listener was closed %d times, want 1", got)
		}
	}
	if winners != 1 {
		t.Fatalf("successful registrations = %d, want 1", winners)
	}
	if err := handler.closeAll(); err != nil {
		t.Fatalf("close handler: %v", err)
	}
	for i, listener := range listeners {
		if got := listener.closed.Load(); got != 1 {
			t.Errorf("listener %d was closed %d times after cleanup, want 1", i, got)
		}
	}
}

func TestTCPForwardingStaleAcceptLoopCannotRemoveReplacement(t *testing.T) {
	handler := NewTCPForwardingHandler()
	first := &tcpForwardListener{listener: &recordingListener{}, bindAddr: "127.0.0.1:12345"}
	second := &tcpForwardListener{listener: &recordingListener{}, bindAddr: first.bindAddr}
	if err := handler.registerListener(first); err != nil {
		t.Fatalf("register first listener: %v", err)
	}
	if got, ok := handler.takeListener(first.bindAddr); !ok || got != first {
		t.Fatal("failed to remove first listener")
	}
	if err := handler.registerListener(second); err != nil {
		t.Fatalf("register replacement listener: %v", err)
	}

	handler.removeListener(first.bindAddr, first)
	if got, ok := handler.takeListener(second.bindAddr); !ok || got != second {
		t.Fatal("stale listener cleanup removed its replacement")
	}
	if err := closeListener(first.listener); err != nil {
		t.Fatalf("close first listener: %v", err)
	}
	if err := closeListener(second.listener); err != nil {
		t.Fatalf("close second listener: %v", err)
	}
}

func TestTCPForwardingCloseClosesAcceptedConnections(t *testing.T) {
	handler := NewTCPForwardingHandler()
	serverConn, peerConn := net.Pipe()
	t.Cleanup(func() { _ = peerConn.Close() })
	tracked, ok := handler.trackConnection(serverConn)
	if !ok {
		t.Fatal("track connection failed")
	}
	if err := peerConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set peer deadline: %v", err)
	}
	if err := handler.closeAll(); err != nil {
		t.Fatalf("close handler: %v", err)
	}
	handler.untrackConnection(tracked)

	buf := make([]byte, 1)
	if _, err := peerConn.Read(buf); err == nil {
		t.Fatal("accepted connection remained open after handler cleanup")
	}
	lateListener := &recordingListener{}
	if err := handler.registerListener(&tcpForwardListener{
		listener: lateListener,
		bindAddr: "127.0.0.1:12345",
	}); err == nil {
		t.Fatal("closed handler accepted a new listener")
	}
	if got := lateListener.closed.Load(); got != 1 {
		t.Fatalf("listener rejected during shutdown was closed %d times, want 1", got)
	}
}
