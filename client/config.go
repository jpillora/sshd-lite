// Package client implements the sshd-lite SSH and Mosh terminal client.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

type Config struct {
	Destination string   `opts:"mode=arg,name=destination,help=user@host or user@host:port"`
	Command     []string `opts:"mode=arg,name=command,help=optional remote command (SSH only)"`
	Port        string   `opts:"short=p,help=SSH port,default=22"`
	Identity    string   `opts:"short=i,help=private key file"`
	Password    string   `opts:"help=password (otherwise prompt when needed)"`
	KnownHosts  string   `opts:"name=known-hosts,help=known_hosts file,default=~/.ssh/known_hosts"`
	Insecure    bool     `opts:"help=skip server host-key verification"`
	Mosh        bool     `opts:"name=mosh,help=use SSH to obtain a key then run the terminal over UDP"`
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

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func promptPassword(prompt string) (string, error) {
	tty, err := openTTY()
	if err != nil {
		return "", fmt.Errorf("%s: no terminal; use --password or --identity", prompt)
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	return string(b), err
}

func (c Config) connect(ctx context.Context) (*ssh.Client, error) {
	username, address, err := c.target()
	if err != nil {
		return nil, err
	}
	var hostKey ssh.HostKeyCallback
	if c.Insecure {
		hostKey = ssh.InsecureIgnoreHostKey()
	} else {
		path := c.KnownHosts
		if path == "" {
			path = "~/.ssh/known_hosts"
		}
		hostKey, err = knownhosts.New(expandHome(path))
		if err != nil {
			return nil, fmt.Errorf("load known hosts (use --known-hosts or explicitly --insecure): %w", err)
		}
	}
	var auth []ssh.AuthMethod
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		agentConn, err := net.DialTimeout("unix", socket, 3*time.Second)
		if err == nil {
			defer agentConn.Close()
			auth = append(auth, ssh.PublicKeysCallback(agent.NewClient(agentConn).Signers))
		}
	}
	paths := []string{c.Identity}
	if c.Identity == "" {
		paths = []string{"~/.ssh/id_ed25519", "~/.ssh/id_rsa"}
	}
	for _, path := range paths {
		b, err := os.ReadFile(expandHome(path))
		if err != nil {
			if c.Identity != "" {
				return nil, err
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(b)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && c.Identity != "" {
			pass, promptErr := promptPassword("Private key passphrase: ")
			if promptErr != nil {
				return nil, promptErr
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass))
		}
		if err != nil {
			if c.Identity != "" {
				return nil, err
			}
			continue
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	auth = append(auth, ssh.PasswordCallback(func() (string, error) {
		if c.Password != "" {
			return c.Password, nil
		}
		return promptPassword(username + "@" + address + " password: ")
	}))
	transport, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { transport.Close() })
	defer stop()
	_ = transport.SetDeadline(time.Now().Add(30 * time.Second))
	conn, channels, requests, err := ssh.NewClientConn(transport, address, &ssh.ClientConfig{User: username, Auth: auth, HostKeyCallback: hostKey})
	if err != nil {
		transport.Close()
		return nil, err
	}
	_ = transport.SetDeadline(time.Time{})
	return ssh.NewClient(conn, channels, requests), nil
}

// Run connects and runs a remote command or terminal, returning its exit status.
func Run(ctx context.Context, c Config, stdin *os.File, stdout, stderr io.Writer) (int, error) {
	if c.Mosh && len(c.Command) != 0 {
		return 0, errors.New("mosh supports interactive shells; omit the command")
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if c.Mosh {
		return runMosh(ctx, conn, stdin, stdout)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	session, err := conn.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()
	session.Stdin, session.Stdout, session.Stderr = stdin, stdout, stderr
	if len(c.Command) > 0 {
		err = session.Run(strings.Join(c.Command, " "))
	} else {
		cols, rows := terminalSize(stdin)
		if term.IsTerminal(int(stdin.Fd())) {
			restore, err := rawTerminal(stdin)
			if err != nil {
				return 0, err
			}
			defer restore()
		}
		terminal := os.Getenv("TERM")
		if terminal == "" {
			terminal = "xterm-256color"
		}
		if err := session.RequestPty(terminal, rows, cols, ssh.TerminalModes{}); err != nil {
			return 0, err
		}
		cancelResize := watchResize(ctx, stdin, func(cols, rows int) { _ = session.WindowChange(rows, cols) })
		defer cancelResize()
		if err = session.Shell(); err == nil {
			err = session.Wait()
		}
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var status *ssh.ExitError
	if errors.As(err, &status) {
		return status.ExitStatus(), nil
	}
	return 0, err
}
