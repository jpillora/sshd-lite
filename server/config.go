package sshd

import (
	"log/slog"

	"golang.org/x/crypto/ssh"
)

// Config is the configuration for the server
type Config struct {
	Host          string       `opts:"help=listening interface (defaults to all)"`
	Port          string       `opts:"short=p,help=listening port (defaults to 22 then fallsback to 2200)"`
	Shell         string       `opts:"help=the shell to use for remote sessions, env=SHELL,default=bash/powershell"`
	KeyFile       string       `opts:"name=keyfile,help=a filepath to a private key (for example an 'id_rsa' file)"`
	KeySeed       string       `opts:"name=keyseed,help=a string to use to seed key generation"`
	AuthType      string       `opts:"mode=arg,name=auth"`
	Auth          []ssh.Auth   `opts:"-"`
	KeepAlive     int          `opts:"name=keepalive,help=server keep alive interval seconds (0 to disable)"`
	IgnoreEnv     bool         `opts:"name=noenv,help=ignore environment variables provided by the client"`
	LogVerbose    bool         `opts:"name=verbose,short=v,help=verbose logs"`
	LogQuiet      bool         `opts:"name=quiet,short=q,help=no logs"`
	Logger        *slog.Logger `opts:"-"`
	SFTP          bool         `opts:"short=s,help=enable the SFTP subsystem (disabled by default)"`
	TCPForwarding bool         `opts:"name=tcp-forwarding,short=t,help=enable TCP forwarding (both local and reverse; disabled by default)"`
}

// NewConfig creates a new Config
func NewConfig(keyFile string, keySeed string) *Config {
	return &Config{
		KeyFile: keyFile,
		KeySeed: keySeed,
	}
}
