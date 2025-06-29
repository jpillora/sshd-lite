package sshd

import (
	"io"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// startSFTPServer starts the SFTP server for the given connection.
func (s *Server) startSFTPServer(connection ssh.Channel) {
	defer connection.Close()
	opts := []sftp.ServerOption{}
	if d, err := os.UserHomeDir(); err == nil {
		opts = append(opts, sftp.WithServerWorkingDirectory(d))
	}
	if s.cli.LogVerbose {
		opts = append(opts, sftp.WithDebug(os.Stderr))
	}
	sftpServer, err := sftp.NewServer(
		connection,
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
