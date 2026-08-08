package xssh

import (
	"encoding/binary"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestHandleSubsystemRequestRejectsMalformedAndUnknownNames(t *testing.T) {
	called := 0
	conn := NewConn(nil, nil, nil, &Config{
		SubsystemHandlers: map[string]SubsystemHandler{
			SFTPSubsystem: func(*Session, *Request) error {
				called++
				return nil
			},
		},
	}).(*xconn)
	sess := &Session{}
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "missing length", payload: []byte{0, 0, 0}},
		{name: "short name", payload: append([]byte{0, 0, 0, 5}, []byte("sftp")...)},
		{name: "trailing bytes", payload: append([]byte{0, 0, 0, 4}, []byte("sftp-extra")...)},
		{name: "unknown name", payload: marshalSubsystemName("future-subsystem")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if conn.handleSubsystemRequest(sess, WrapRequest(&ssh.Request{Payload: test.payload})) {
				t.Fatal("malformed or unknown subsystem was accepted")
			}
		})
	}
	if called != 0 {
		t.Fatalf("SFTP handler called %d times", called)
	}
	if !conn.handleSubsystemRequest(sess, WrapRequest(&ssh.Request{Payload: marshalSubsystemName(SFTPSubsystem)})) {
		t.Fatal("valid SFTP subsystem was rejected")
	}
	if called != 1 {
		t.Fatalf("SFTP handler called %d times after valid request", called)
	}
}

func marshalSubsystemName(name string) []byte {
	payload := make([]byte, 4+len(name))
	binary.BigEndian.PutUint32(payload, uint32(len(name)))
	copy(payload[4:], name)
	return payload
}

// TestNewConnDoesNotMutateConfig guards the invariant that makes per-connection
// defaulting safe: servers hand the same *Config to every NewConn, so resolving
// the shell path in place would race across concurrent connections.
func TestNewConnDoesNotMutateConfig(t *testing.T) {
	// Session enabled with an unset Shell is what triggers the resolution.
	shared := &Config{Session: true}
	first := NewConn(nil, nil, nil, shared)
	second := NewConn(nil, nil, nil, shared)

	if shared.Shell != "" {
		t.Fatalf("NewConn wrote to the caller's Config.Shell: %q", shared.Shell)
	}
	resolved := first.Config().Shell
	if resolved == "" {
		t.Skip("no shell on this machine, nothing was resolved")
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("Shell not resolved to an absolute path: %q", resolved)
	}
	if got := second.Config().Shell; got != resolved {
		t.Fatalf("connections disagree on shell: %q vs %q", resolved, got)
	}
}

func TestServeRejectsOverlapWhileAwaitingCooperativeHandler(t *testing.T) {
	channels := make(chan ssh.NewChannel, 1)
	requests := make(chan *ssh.Request)
	accepted := newControlledSSHChannel()
	handlerStarted := make(chan struct{})
	handlerFinished := make(chan struct{})
	handlerErr := make(chan error, 1)

	conn := NewConn(nil, channels, requests, &Config{
		ChannelHandlers: map[string]ChannelHandler{
			"controlled": func(_ Conn, ch ssh.NewChannel) error {
				channel, _, err := ch.Accept()
				if err != nil {
					handlerErr <- err
					return err
				}
				close(handlerStarted)
				<-accepted.closed
				if err := channel.Close(); err != nil {
					handlerErr <- err
					return err
				}
				close(handlerFinished)
				return nil
			},
		},
	}).(*xconn)

	firstDone := make(chan struct{})
	go func() {
		conn.Serve()
		close(firstDone)
	}()
	channels <- &controlledNewChannel{channel: accepted}
	awaitConnTestSignal(t, handlerStarted, "custom channel handler start")
	close(channels)
	close(requests)

	// Wait until the first Serve has closed child admission but is deliberately
	// held in its Wait by the cooperative handler.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn.routineMu.Lock()
		draining := conn.serveActive && !conn.acceptingRoutines
		conn.routineMu.Unlock()
		if draining {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Serve did not enter its draining state")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-firstDone:
		t.Fatal("Serve returned before its custom channel handler finished")
	default:
	}

	secondDone := make(chan struct{})
	go func() {
		conn.Serve()
		close(secondDone)
	}()
	awaitConnTestSignal(t, secondDone, "overlapping Serve rejection")
	select {
	case <-firstDone:
		t.Fatal("overlapping Serve disturbed the active Serve lifecycle")
	default:
	}

	_ = accepted.Close()
	awaitConnTestSignal(t, handlerFinished, "cooperative handler teardown")
	awaitConnTestSignal(t, firstDone, "first Serve completion")
	select {
	case err := <-handlerErr:
		t.Fatalf("custom handler failed: %v", err)
	default:
	}
}

type controlledNewChannel struct {
	channel *controlledSSHChannel
}

func (c *controlledNewChannel) Accept() (ssh.Channel, <-chan *ssh.Request, error) {
	requests := make(chan *ssh.Request)
	close(requests)
	return c.channel, requests, nil
}

func (c *controlledNewChannel) Reject(ssh.RejectionReason, string) error { return nil }
func (c *controlledNewChannel) ChannelType() string                      { return "controlled" }
func (c *controlledNewChannel) ExtraData() []byte                        { return nil }

type controlledSSHChannel struct {
	closed chan struct{}
	once   sync.Once
}

func newControlledSSHChannel() *controlledSSHChannel {
	return &controlledSSHChannel{closed: make(chan struct{})}
}

func (c *controlledSSHChannel) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *controlledSSHChannel) Write(p []byte) (int, error) { return len(p), nil }
func (c *controlledSSHChannel) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
func (c *controlledSSHChannel) CloseWrite() error { return nil }
func (c *controlledSSHChannel) SendRequest(string, bool, []byte) (bool, error) {
	return false, nil
}
func (c *controlledSSHChannel) Stderr() io.ReadWriter { return controlledReadWriter{} }

type controlledReadWriter struct{}

func (controlledReadWriter) Read([]byte) (int, error)    { return 0, io.EOF }
func (controlledReadWriter) Write(p []byte) (int, error) { return len(p), nil }

func awaitConnTestSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
