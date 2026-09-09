package sshd

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
)

type attachmentCloser func() error

func (f attachmentCloser) Close() error { return f() }

func TestAttachmentFailureClosesListeners(t *testing.T) {
	var closed atomic.Bool
	failure := errors.New("companion bind failed")
	attach := func(context.Context, net.Addr) (xssh.ExecHandler, io.Closer, error) {
		return nil, attachmentCloser(func() error { closed.Store(true); return nil }), failure
	}
	s := newLifecycleTestServer(t, Config{Attach: attach})
	l := listenLifecycleTest(t)
	defer l.Close()
	if err := s.StartWithContext(context.Background(), l); !errors.Is(err, failure) {
		t.Fatalf("attachment error: %v", err)
	}
	if !closed.Load() {
		t.Fatal("partial attachment was not closed")
	}
	if c, err := net.DialTimeout("tcp", l.Addr().String(), time.Second); err == nil {
		c.Close()
		t.Fatal("SSH listener left open after attachment failure")
	}
}

func TestAttachmentsHaveIndependentListenerLifetimes(t *testing.T) {
	stopped := make(chan string, 2)
	attach := func(_ context.Context, addr net.Addr) (xssh.ExecHandler, io.Closer, error) {
		address := addr.String()
		handler := func(s *xssh.Session, command string) (bool, error) {
			if command != "attachment-address" {
				return false, nil
			}
			defer s.Channel.Close()
			if _, err := io.WriteString(s.Channel, address); err != nil {
				return true, err
			}
			_, err := s.Channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			return true, err
		}
		return handler, attachmentCloser(func() error { stopped <- address; return nil }), nil
	}
	s := newLifecycleTestServer(t, Config{Attach: attach})
	type run struct {
		listener net.Listener
		cancel   context.CancelFunc
		done     chan error
	}
	runs := make([]run, 2)
	for i := range runs {
		l := listenLifecycleTest(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		runs[i] = run{l, cancel, done}
		go func() { done <- s.StartWithContext(ctx, l) }()
		t.Cleanup(func() { cancel(); l.Close() })
	}
	check := func(address string) {
		t.Helper()
		c := dialLifecycleTest(t, address)
		defer c.Close()
		session, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		reply, err := session.Output("attachment-address")
		if err != nil || string(reply) != address {
			t.Fatalf("listener handler leaked: %q %v", reply, err)
		}
	}
	for _, r := range runs {
		check(r.listener.Addr().String())
	}
	for i, r := range runs {
		// An attachment must also stop on listener failure without ctx cancellation.
		if i == 0 {
			r.listener.Close()
		} else {
			r.cancel()
		}
		select {
		case err := <-r.done:
			if (err != nil) != (i == 0) {
				t.Errorf("unexpected listener result: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("attachment shutdown stalled")
		}
		select {
		case addr := <-stopped:
			if addr != r.listener.Addr().String() {
				t.Fatalf("wrong attachment stopped: %s", addr)
			}
		case <-time.After(time.Second):
			t.Fatal("attachment closer missing")
		}
		if i == 0 {
			check(runs[1].listener.Addr().String())
		}
	}
	if s.xsshConfig.ExecHandler != nil {
		t.Fatal("attachment mutated reusable base handler")
	}
}
