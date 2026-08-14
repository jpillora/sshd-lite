// Package xssh provides a symmetric SSH connection handler that works for both
// server and client connections. It abstracts the handling of global requests,
// channels, and sessions so that either side of an SSH connection can offer
// services to the other.
package xssh

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"

	"golang.org/x/crypto/ssh"
)

// Built-in SSH handler names. Keeping these names next to Config and using
// them at each registration site lets higher-level servers validate custom
// handlers against the behavior NewConn will enable.
const (
	SessionChannelType            = "session"
	DirectTCPIPChannelType        = "direct-tcpip"
	PTYRequestType                = "pty-req"
	WindowChangeRequestType       = "window-change"
	EnvRequestType                = "env"
	ShellRequestType              = "shell"
	ExecRequestType               = "exec"
	SFTPSubsystem                 = "sftp"
	TCPIPForwardRequestType       = "tcpip-forward"
	CancelTCPIPForwardRequestType = "cancel-tcpip-forward"
)

// Config is the configuration for an xssh.Conn.
// It provides handlers for global requests, channels, and session requests.
type Config struct {
	// Logger for debug and error messages. If nil, logging is disabled.
	Logger *slog.Logger
	// KeepAlive interval in seconds. If > 0, sends periodic ping requests.
	KeepAlive int
	// IgnoreEnv if true, ignores environment variables from "env" requests.
	// It has no bearing on the server's own environment; see InheritEnv.
	IgnoreEnv bool
	// InheritEnv if true, starts every session from the server process's
	// environment. Off by default: shells and commands run as the user that
	// started sshd-lite, so inheriting hands each authenticated client whatever
	// the operator exported, secrets included. When false a session gets only
	// the variables a shell needs to function.
	InheritEnv bool
	// WorkingDirectory sets the initial working directory for sessions and sftp.
	WorkingDirectory string
	// Shell is the shell executable to use for sessions. Defaults to "bash".
	Shell string
	// Session enables the built-in session handling (shell, exec, PTY).
	Session bool
	// Handlers run synchronously as connection-owned work. Serve waits for them
	// to return, so they must stop promptly when their Conn, Session, or channel
	// is closed.
	GlobalRequestHandlers  map[string]GlobalRequestHandler
	ChannelHandlers        map[string]ChannelHandler
	SessionRequestHandlers map[string]SessionRequestHandler
	SubsystemHandlers      map[string]SubsystemHandler
	// SFTP enables the SFTP subsystem handler.
	SFTP bool
	// LocalForwarding enables direct-tcpip channel handling (client requests server to connect).
	LocalForwarding bool
	// RemoteForwarding enables tcpip-forward global request handling (client requests server to listen).
	RemoteForwarding bool
}

// GlobalRequestHandler handles global (connection-level) SSH requests.
// Return an error to reject the request; return nil to accept.
// Call req.Reply() to send a custom reply; otherwise auto-reply is sent.
// The handler runs synchronously in connection-owned work and must return
// promptly when conn closes; Conn.Serve waits for it during shutdown.
type GlobalRequestHandler func(conn Conn, req *Request) error

// ChannelHandler handles new SSH channel requests.
// Return an error to reject the channel; return nil to accept.
// The handler runs synchronously in connection-owned work and must return
// promptly when conn or the accepted channel closes; Conn.Serve waits for it.
type ChannelHandler func(conn Conn, ch ssh.NewChannel) error

// SessionRequestHandler handles requests within an SSH session.
// Return an error to reject the request; return nil to accept.
// Call req.Reply() to send a custom reply; otherwise auto-reply is sent.
// The handler runs synchronously in session-owned work and must return promptly
// when the Session closes; Conn.Serve waits for it during shutdown.
type SessionRequestHandler func(sess *Session, req *Request) error

// SubsystemHandler handles subsystem requests (e.g., sftp).
// Return an error to reject the request; return nil to accept.
// The handler runs synchronously in session-owned work and must return promptly
// when the Session closes; Conn.Serve waits for it during shutdown.
type SubsystemHandler func(sess *Session, req *Request) error

// ShellPath returns the full path to a shell executable.
// If shell is empty, defaults to "powershell" on Windows and "bash" otherwise.
// Returns an error if the shell cannot be found.
func ShellPath(shell string) (string, error) {
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "powershell"
		} else {
			shell = "bash"
		}
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return "", fmt.Errorf("failed to find shell: %s", shell)
	}
	return path, nil
}
