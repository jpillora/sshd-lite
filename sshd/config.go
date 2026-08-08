package sshd

import (
	"context"
	"log/slog"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

const (
	// DefaultHandshakeTimeout bounds the time an unauthenticated client may
	// spend completing the SSH handshake.
	DefaultHandshakeTimeout = 10 * time.Second
	// DefaultMaxPendingHandshakes bounds the number of SSH handshakes that may
	// run concurrently.
	DefaultMaxPendingHandshakes = 64
)

// Config is the configuration for the server
type Config struct {
	Host      string `opts:"help=listening interface (defaults to all)"`
	Port      string `opts:"short=p,help=listening port (defaults to 22 then fallsback to 2200)"`
	Shell     string `opts:"help=the shell to use for remote sessions, env=SHELL,default=bash/powershell"`
	WorkDir   string `opts:"name=workdir,help=working directory for sessions,default=current directory"`
	KeyFile   string `opts:"name=keyfile,help=a filepath to a private key (for example an 'id_rsa' file)"`
	KeySeed   string `opts:"name=keyseed,env,help=a string to use to seed key generation"`
	KeySeedEC bool   `opts:"name=keyseed-ec,env,help=use elliptic curve for key generation"`
	AuthType  string `opts:"mode=arg,name=auth"`
	KeepAlive int    `opts:"name=keepalive,help=server keep alive interval seconds (0 to disable)"`
	// HandshakeTimeout is the maximum time allowed for an SSH handshake.
	// Zero selects DefaultHandshakeTimeout; a negative duration disables it.
	HandshakeTimeout time.Duration `opts:"name=handshake-timeout,help=maximum time for an SSH handshake (negative to disable)"`
	// MaxPendingHandshakes is the maximum number of SSH handshakes allowed at
	// once. Zero selects DefaultMaxPendingHandshakes; a negative value disables it.
	MaxPendingHandshakes int  `opts:"name=max-pending-handshakes,help=maximum concurrent unauthenticated SSH handshakes (negative to disable)"`
	IgnoreEnv            bool `opts:"name=noenv,help=ignore environment variables provided by the client"`
	LogVerbose           bool `opts:"name=verbose,short=v,help=verbose logs"`
	LogQuiet             bool `opts:"name=quiet,short=q,help=no logs"`
	SFTP                 bool `opts:"short=s,help=enable the SFTP subsystem (disabled by default)"`
	TCPForwarding        bool `opts:"name=tcp-forwarding,short=t,help=enable TCP forwarding (both local and reverse; disabled by default)"`
	// programmatic options
	KeyBytes []byte          `opts:"-"`
	Logger   *slog.Logger    `opts:"-"`
	AuthKeys []ssh.PublicKey `opts:"-"`
	// ConnectionHandler is called when a new SSH connection is established.
	// The context is cancelled when the connection closes. The handler runs
	// asynchronously and is not awaited during server shutdown.
	ConnectionHandler func(context.Context, *ssh.ServerConn) `opts:"-"`
	// Protocol handlers are synchronous connection-owned work. Server shutdown
	// waits for them, so they must return promptly when their connection,
	// session, or channel closes.
	GlobalRequestHandlers  map[string]xssh.GlobalRequestHandler  `opts:"-"`
	ChannelHandlers        map[string]xssh.ChannelHandler        `opts:"-"`
	SessionRequestHandlers map[string]xssh.SessionRequestHandler `opts:"-"`
	SubsystemHandlers      map[string]xssh.SubsystemHandler      `opts:"-"`
}

// Session is an alias for xssh.Session for backwards compatibility
type Session = xssh.Session

// Request is an alias for xssh.Request for backwards compatibility
type Request = xssh.Request

// Wrap wraps an ssh.Request in an xssh.Request
func Wrap(req *ssh.Request) *xssh.Request {
	return xssh.WrapRequest(req)
}

// Handler type aliases for backwards compatibility
type GlobalRequestHandler = xssh.GlobalRequestHandler
type ChannelHandler = xssh.ChannelHandler
type SessionRequestHandler = xssh.SessionRequestHandler
type SubsystemHandler = xssh.SubsystemHandler
