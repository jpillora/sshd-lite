package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// authOwner keeps the agent alive through handshake signatures only.
type authOwner struct {
	conn net.Conn
	stop func() bool
	done chan struct{}
}

func (a *authOwner) close() {
	if a.conn == nil {
		return
	}
	if !a.stop() {
		<-a.done
	}
	_ = a.conn.Close()
}
func (a *authOwner) agent(ctx context.Context, socket string) ([]ssh.Signer, error) {
	if a.conn != nil {
		return nil, errors.New("ssh client: repeated agent discovery")
	}
	if socket == "" {
		return nil, nil
	}
	c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	a.conn = c
	a.done = make(chan struct{})
	a.stop = context.AfterFunc(ctx, func() { _ = c.Close(); close(a.done) })
	if err := c.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	// Hide Closer so canonical RPCs own no reader goroutine.
	rw := struct {
		io.Reader
		io.Writer
	}{io.LimitReader(c, 1<<20), c}
	signers, err := agent.NewClient(rw).Signers()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("ssh client: agent keys: %w", err)
	}
	if len(signers) > 256 {
		return nil, errors.New("ssh client: agent identity limit")
	}
	return signers, nil
}
