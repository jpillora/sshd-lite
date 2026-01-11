package sshd

import (
	"io"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// NewSFTPHandler creates a new SFTP subsystem handler
func NewSFTPHandler(s *Server) SubsystemHandler {
	return func(channel ssh.Channel, req *Request) error {
		s.debugf("SFTP subsystem request accepted")
		go startSFTPServer(s, channel)
		return nil
	}
}

// startSFTPServer starts the SFTP server for the given connection.
func startSFTPServer(s *Server, channel ssh.Channel) {
	defer channel.Close()
	opts := []sftp.ServerOption{}
	if d, err := os.UserHomeDir(); err == nil {
		opts = append(opts, sftp.WithServerWorkingDirectory(d))
	}
	if s.config.LogVerbose {
		opts = append(opts, sftp.WithDebug(os.Stderr))
	}
	sftpServer, err := sftp.NewServer(
		channel,
		opts...,
	)
	if err != nil {
		s.debugf("Failed to create SFTP server: %v", err)
		return
	}
	if err := sftpServer.Serve(); err != nil && err != io.EOF {
		s.debugf("SFTP request error: %s", err)
	} else {
		s.debugf("SFTP request served")
	}
}
