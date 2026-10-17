package client

import (
	"errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
)

func prepareTrust(o DialOptions) (ssh.HostKeyCallback, error) {
	var files []string
	for _, name := range o.resolved.known {
		info, err := os.Stat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return nil, errors.New("ssh client: nonregular or oversized known_hosts")
		}
		files = append(files, name)
	}
	if len(files) == 0 {
		return nil, errors.New("ssh client: host-key verification requires known_hosts")
	}
	check, err := knownhosts.New(files...)
	if err != nil {
		return nil, err
	}
	return func(_ string, remote net.Addr, key ssh.PublicKey) error {
		return check(net.JoinHostPort(o.resolved.alias, o.Port), remote, key)
	}, nil
}
