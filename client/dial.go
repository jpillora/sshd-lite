package client

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jpillora/sshd-lite/internal/sshconn"
	"golang.org/x/crypto/ssh"
)

// DialOptions supplies a connection's authority and authentication explicitly.
// Dial never reads the process environment. The caller must not mutate options
// or authentication backing data during Dial. The returned client is caller-owned.
type DialOptions struct {
	Host, User, Port string
	Auth             []ssh.AuthMethod
	HostKeyCallback  ssh.HostKeyCallback
	// Timeout bounds TCP establishment. Zero selects ten seconds.
	Timeout time.Duration
}

// Dial establishes an authenticated upstream x/crypto/ssh connection. Context
// cancellation interrupts establishment; after success the caller owns both
// connection lifetime and cancellation of its sessions.
func Dial(ctx context.Context, options DialOptions) (*ssh.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Host == "" || strings.ContainsAny(options.Host, "\x00\r\n\t /\\") ||
		options.User == "" || strings.ContainsAny(options.User, "\x00\r\n") {
		return nil, errors.New("ssh client: host and user are required")
	}
	port := options.Port
	if port == "" {
		port = "22"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, errors.New("ssh client: invalid port")
	}
	if options.Timeout < 0 {
		return nil, errors.New("ssh client: negative connection timeout")
	}
	if options.HostKeyCallback == nil {
		return nil, errors.New("ssh client: host-key verification is required")
	}
	return sshconn.Dial(ctx, net.JoinHostPort(options.Host, port), &ssh.ClientConfig{
		User: options.User, Auth: append([]ssh.AuthMethod(nil), options.Auth...),
		HostKeyCallback: options.HostKeyCallback, Timeout: options.Timeout,
	})
}
