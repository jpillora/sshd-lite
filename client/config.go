// Package client implements the sshd-lite SSH client.
package client

import (
	"errors"
	"net"
	"os/user"
	"strings"
)

type Config struct {
	Destination     string   `opts:"mode=arg,name=destination,help=user@host or user@host:port"`
	Command         []string `opts:"mode=arg,name=command,help=optional remote command"`
	Port            string   `opts:"short=p,help=SSH port,default=22"`
	Identity        string   `opts:"short=i,help=private key file"`
	Password        string   `opts:"help=password (otherwise prompt when needed)"`
	KnownHosts      string   `opts:"name=known-hosts,help=known_hosts file (defaults to private XDG state)"`
	ShareKnownHosts bool     `opts:"name=share-known-hosts,help=use ~/.ssh/known_hosts instead of private client state"`
	StrictHosts     bool     `opts:"name=strict-hosts,help=confirm unknown host keys instead of adding them automatically"`
	Insecure        bool     `opts:"help=skip server host-key verification"`
	Accept          bool     `opts:"help=automatically add or replace the server host key"`
}

func (c Config) target() (string, string, error) {
	destination := c.Destination
	username := ""
	if i := strings.LastIndex(destination, "@"); i >= 0 {
		username, destination = destination[:i], destination[i+1:]
	}
	if username == "" {
		u, err := user.Current()
		if err != nil {
			return "", "", err
		}
		username = u.Username
	}
	if destination == "" {
		return "", "", errors.New("destination is required")
	}
	port := c.Port
	if port == "" {
		port = "22"
	}
	host, embeddedPort, err := net.SplitHostPort(destination)
	if err == nil {
		destination = host
		if c.Port == "" {
			port = embeddedPort
		}
	} else {
		destination = strings.TrimSuffix(strings.TrimPrefix(destination, "["), "]")
	}
	return username, net.JoinHostPort(destination, port), nil
}
