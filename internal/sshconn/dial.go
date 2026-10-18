// Package sshconn owns context-aware SSH dialing and handshake deadlines.
package sshconn

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

func Dial(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	return DialWithSetup(ctx, address, config, nil)
}

// DialWithSetup allows the authentication owner to suspend the owned transport
// deadline during synchronous interaction. Setup runs before the handshake.
func DialWithSetup(ctx context.Context, address string, config *ssh.ClientConfig, setup func(net.Conn)) (*ssh.Client, error) {
	if config == nil || config.HostKeyCallback == nil {
		return nil, fmt.Errorf("SSH configuration with a host-key callback is required")
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	transport, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { transport.Close(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	if err := transport.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		transport.Close()
		return nil, err
	}
	if setup != nil {
		setup(transport)
	}
	conn, channels, requests, err := ssh.NewClientConn(transport, address, config)
	if err != nil {
		transport.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if ctx.Err() != nil {
		transport.Close()
		return nil, ctx.Err()
	}
	if err := transport.SetDeadline(time.Time{}); err != nil {
		transport.Close()
		return nil, err
	}
	return ssh.NewClient(conn, channels, requests), nil
}
