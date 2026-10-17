package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestDialCancellationDuringHandshake(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, err := Dial(ctx, DialOptions{Host: host, Port: port, User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	c := <-accepted
	if c == nil {
		t.Fatal("accept failed")
	}
	defer c.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handshake ignored cancellation")
	}
}

func TestDialRejectsMissingTrust(t *testing.T) {
	_, err := Dial(context.Background(), DialOptions{Host: "127.0.0.1", User: "test"})
	if err == nil {
		t.Fatal("missing host verification accepted")
	}
}
