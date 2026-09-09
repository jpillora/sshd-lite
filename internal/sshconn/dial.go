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
	stop := context.AfterFunc(ctx, func() { transport.Close() })
	defer stop()
	_ = transport.SetDeadline(time.Now().Add(30 * time.Second))
	conn, channels, requests, err := ssh.NewClientConn(transport, address, config)
	if err != nil {
		transport.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	_ = transport.SetDeadline(time.Time{})
	return ssh.NewClient(conn, channels, requests), nil
}
