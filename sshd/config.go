package sshd

import (
	"errors"
	"log/slog"

	"golang.org/x/crypto/ssh"
)

// Config is the configuration for the server
type Config struct {
	Host          string `opts:"help=listening interface (defaults to all)"`
	Port          string `opts:"short=p,help=listening port (defaults to 22 then fallsback to 2200)"`
	Shell         string `opts:"help=the shell to use for remote sessions, env=SHELL,default=bash/powershell"`
	WorkDir       string `opts:"name=workdir,help=working directory for sessions,default=current directory"`
	KeyFile       string `opts:"name=keyfile,help=a filepath to a private key (for example an 'id_rsa' file)"`
	KeySeed       string `opts:"name=keyseed,env,help=a string to use to seed key generation"`
	KeySeedEC     bool   `opts:"name=keyseed-ec,env,help=use elliptic curve for key generation"`
	AuthType      string `opts:"mode=arg,name=auth"`
	KeepAlive     int    `opts:"name=keepalive,help=server keep alive interval seconds (0 to disable)"`
	IgnoreEnv     bool   `opts:"name=noenv,help=ignore environment variables provided by the client"`
	LogVerbose    bool   `opts:"name=verbose,short=v,help=verbose logs"`
	LogQuiet      bool   `opts:"name=quiet,short=q,help=no logs"`
	SFTP          bool   `opts:"short=s,help=enable the SFTP subsystem (disabled by default)"`
	TCPForwarding bool   `opts:"name=tcp-forwarding,short=t,help=enable TCP forwarding (both local and reverse; disabled by default)"`
	// programmatic options
	KeyBytes               []byte                           `opts:"-"`
	Logger                 *slog.Logger                     `opts:"-"`
	AuthKeys               []ssh.PublicKey                  `opts:"-"`
	GlobalRequestHandlers  map[string]GlobalRequestHandler  `opts:"-"`
	ChannelHandlers        map[string]ChannelHandler        `opts:"-"`
	SessionRequestHandlers map[string]SessionRequestHandler `opts:"-"`
	SubsystemHandlers      map[string]SubsystemHandler      `opts:"-"`
}

// Handler types for extensibility

// GlobalRequestHandler handles global (connection-level) SSH requests.
// Return an error to reject the request; return nil to accept.
// Call req.Reply() to send a custom reply; otherwise auto-reply is sent.
type GlobalRequestHandler func(conn ssh.Conn, req *Request) error

// ChannelHandler handles new SSH channel requests.
// Return an error to reject the channel; return nil to accept.
type ChannelHandler func(ch ssh.NewChannel) error

// SessionRequestHandler handles requests within an SSH session.
// Return an error to reject the request; return nil to accept.
// Call req.Reply() to send a custom reply; otherwise auto-reply is sent.
type SessionRequestHandler func(sess *Session, req *Request) error

// SubsystemHandler handles subsystem requests (e.g., sftp).
// Return an error to reject the request; return nil to accept.
type SubsystemHandler func(ch ssh.Channel, req *Request) error

// Request wraps ssh.Request to track whether Reply was called.
type Request struct {
	*ssh.Request
	replied bool
}

// Wrap creates a wrapped request that tracks whether Reply was called.
func Wrap(req *ssh.Request) *Request {
	return &Request{Request: req}
}

// Reply sends a reply to the request and marks it as replied.
func (r *Request) Reply(ok bool, payload []byte) error {
	if r.replied {
		return errors.New("request already replied to")
	}
	r.replied = true
	return r.Request.Reply(ok, payload)
}

// Replied returns true if Reply has been called.
func (r *Request) Replied() bool {
	return r.replied
}

// Session represents an active SSH session with its associated state.
type Session struct {
	server *Server

	Channel ssh.Channel
	Env     []string
	Resizes chan []byte
}

// Debugf logs a debug message for this session.
func (s *Session) Debugf(f string, args ...interface{}) {
	s.server.debugf(f, args...)
}

// Errorf logs an error message for this session.
func (s *Session) Errorf(f string, args ...interface{}) {
	s.server.errorf(f, args...)
}

// Config returns the server configuration.
func (s *Session) Config() Config {
	return s.server.config
}
