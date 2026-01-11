package sshtest

import (
	"time"

	"golang.org/x/crypto/ssh"
)

// CreateSSHClient creates an SSH client connection to the given address with no auth.
func CreateSSHClient(addr string) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}
