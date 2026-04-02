package xssh

import (
	"bufio"
	"io"
	"log/slog"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPConfig configures the SFTP subsystem handler.
type SFTPConfig struct {
	// WorkDir is the working directory for SFTP. Defaults to user home.
	WorkDir string
	// Logger enables debug logger
	Logger *slog.Logger
}

// NewSFTPHandler creates a new SFTP subsystem handler.
// Register this as the handler for the "sftp" subsystem.
func NewSFTPHandler(cfg SFTPConfig) SubsystemHandler {
	return func(sess *Session, req *Request) error {
		if cfg.Logger != nil {
			cfg.Logger.Debug("SFTP subsystem request accepted")
		}
		go startSFTPServer(sess, cfg)
		return nil
	}
}

// startSFTPServer starts the SFTP server for the given session.
func startSFTPServer(sess *Session, cfg SFTPConfig) {
	defer sess.Channel.Close()
	opts := []sftp.ServerOption{}
	// Set working directory
	workDir := cfg.WorkDir
	if workDir == "" {
		if d, err := os.UserHomeDir(); err == nil {
			workDir = d
		}
	}
	if workDir != "" {
		opts = append(
			opts,
			sftp.WithServerWorkingDirectory(workDir),
		)
	}
	// Enable debug if logger is set
	debug := func(msg string, args ...any) {
		if cfg.Logger != nil {
			cfg.Logger.Debug(msg, args...)
		}
	}
	if cfg.Logger != nil {
		pr, pw := io.Pipe()
		defer pw.Close()
		go func() {
			scanner := bufio.NewScanner(pr)
			for scanner.Scan() {
				cfg.Logger.Debug(scanner.Text())
			}
		}()
		opts = append(opts, sftp.WithDebug(pw))
	}
	exitStatus := uint32(0)
	defer func() {
		sess.Channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{exitStatus}))
	}()
	sftpServer, err := sftp.NewServer(sess.Channel, opts...)
	if err != nil {
		debug("Failed to create SFTP server", "error", err)
		exitStatus = 1
		return
	}
	if err := sftpServer.Serve(); err != nil && err != io.EOF {
		debug("SFTP server error", "error", err)
		exitStatus = 1
	} else {
		debug("SFTP server exited normally")
	}
}
