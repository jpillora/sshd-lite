package client

import (
	"context"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

type Client struct {
	conn   *ssh.Client
	config *ssh.ClientConfig
}

func NewClient() *Client {
	config := &ssh.ClientConfig{
		User: "user",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	return &Client{
		config: config,
	}
}

func (c *Client) ConnectUnixSocket(socketPath string) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "unix", socketPath)
	if err != nil {
		return err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, socketPath, c.config)
	if err != nil {
		conn.Close()
		return err
	}

	c.conn = ssh.NewClient(sshConn, chans, reqs)
	return nil
}

func (c *Client) NewSession() (*ssh.Session, error) {
	if c.conn == nil {
		return nil, os.ErrInvalid
	}
	return c.conn.NewSession()
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	if c.conn == nil {
		return false, nil, os.ErrInvalid
	}
	return c.conn.SendRequest(name, wantReply, payload)
}