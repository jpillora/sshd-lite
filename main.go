//go:generate go tool github.com/jpillora/md-tmpl -w README.md

package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/jpillora/opts"
	"github.com/jpillora/sshd-lite/client"
	"github.com/jpillora/sshd-lite/mosh"
	"github.com/jpillora/sshd-lite/sshd"
	"github.com/jpillora/sshd-lite/xssh"
)

var version string = "0.0.0-src" //set via ldflags

const authArg = `
<auth> must be set to one of:
1. a username and password string separated by a colon ("myuser:mypass")
2. a path to an ssh authorized_keys file ("~/.ssh/authorized_keys"); entries
   must be unrestricted, and any entry with options makes the file invalid
3. a GitHub user ("github.com/myuser"); public keys are fetched from .keys
4. "none" to disable client authentication :WARNING: very insecure
`

const notes = `
Notes:
* if no keyfile and no keyseed are set, a random 2048-bit RSA key is used
* authorized_keys files are validated at startup and reloaded for every public
  key authentication; a failed reload denies authentication until the file is
  valid again, and any entry with options makes the entire file invalid
* authenticated names do not select system users or change privileges; shells
  and commands run as the user that started sshd-lite
* sessions inherit the process environment; no-inherit-env limits this to
  essential shell variables; /etc/environment supplies defaults on Unix
  unless no-global-env is set
* remote commands stream stdin, stdout, and stderr and report their exit status
* shells, commands, and SFTP start in workdir; if unset, it is the process
  working directory when the server is created
* sftp-wd exposes workdir as SFTP's virtual root instead of only its start dir
* handshake-timeout and max-pending-handshakes use safe defaults when zero;
  set either to a negative value to disable that protection
* MAX_PENDING_HANDSHAKES sets the CLI default for max-pending-handshakes
`

type serverCommand struct {
	sshd.Config
	Mosh    bool   `opts:"name=mosh,help=enable Mosh on the same UDP port (five-minute idle timeout)"`
	Command string `opts:"mode=cmdname"`
}

type clientCommand struct {
	client.Config
	Mosh       bool   `opts:"name=mosh,help=use SSH to obtain a key then run the terminal over UDP"`
	MoshServer string `opts:"name=mosh-server,help=remote mosh-server executable (default mosh-server)"`
}

func newCLI() (opts.Opts, *serverCommand, *clientCommand) {
	server := &serverCommand{Config: sshd.Config{
		Host:                 "0.0.0.0",
		KeepAlive:            60,
		HandshakeTimeout:     sshd.DefaultHandshakeTimeout,
		MaxPendingHandshakes: getEnvInt("MAX_PENDING_HANDSHAKES", sshd.DefaultMaxPendingHandshakes),
	}}
	client := &clientCommand{}
	cli := opts.New(server).
		Name("sshd-lite").
		Version(version).
		Repo("github.com/jpillora/sshd-lite").
		PkgRepo().
		DocAfter("args", "authArg", authArg).
		DocBefore("version", "notes", notes).
		AddCommand(opts.New(client).Name("client").Summary("connect to an SSH server").Version(version))
	return cli, server, client
}

func main() {
	cli, server, client := newCLI()
	cli.ParseArgs(normalizeCLIArgs(os.Args))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if server.Command == "client" {
		code, err := runClient(ctx, client)
		if err != nil {
			log.Print(err)
			code = 1
		}
		os.Exit(code)
	}
	if err := runServer(ctx, server); err != nil {
		log.Fatal(err)
	}
}

func runClient(ctx context.Context, c *clientCommand) (int, error) {
	if !c.Mosh {
		return client.Run(ctx, c.Config, os.Stdin, os.Stdout, os.Stderr)
	}
	conn, err := client.Connect(ctx, c.Config)
	if err != nil {
		return 0, err
	}
	return mosh.Run(ctx, conn, os.Stdin, os.Stdout, mosh.ClientConfig{Server: c.MoshServer, Command: c.Command})
}

func runServer(ctx context.Context, c *serverCommand) error {
	if c.Mosh {
		c.Attach = func(ctx context.Context, addr net.Addr) (xssh.ExecHandler, io.Closer, error) {
			handler, closer, err := mosh.Attach(ctx, addr)
			if err == nil && !c.LogQuiet {
				log.Printf("Listening on udp://%s for mosh connections", addr)
			}
			return handler, closer, err
		}
	} else {
		// The binary knows Mosh but this listener did not enable it. Recognize the
		// standard bootstrap and fail it promptly instead of running a possibly
		// installed external mosh-server under the ordinary exec lifecycle.
		c.Attach = mosh.RejectAttach
	}
	s, err := sshd.NewServer(c.Config)
	if err != nil {
		return err
	}
	if c.Mosh && !c.LogQuiet {
		log.Print("Mosh enabled")
	}
	return s.StartContext(ctx)
}

func normalizeCLIArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	normalized := append([]string(nil), args...)
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(normalized[0], `\`, "/"))), ".exe")
	if name == "ssh-lite" {
		withClient := make([]string, 0, len(normalized)+1)
		withClient = append(withClient, normalized[0], "client")
		normalized = append(withClient, normalized[1:]...)
	}
	clientAt := len(normalized)
	translateEnd := len(normalized)
	flagValues := make(map[int]bool)
	for i := 1; i < len(normalized); i++ {
		if normalized[i] == "--" {
			translateEnd = i
			break
		}
	}
	for i := 1; i < translateEnd; i++ {
		if normalized[i] == "--" {
			break
		}
		if strings.HasPrefix(normalized[i], "-") {
			if strings.Contains(normalized[i], "=") || rootBoolFlag(normalized[i]) {
				continue
			}
			// A non-boolean root flag consumes the following token, even when
			// that value happens to equal the client command name.
			i++
			flagValues[i] = true
			continue
		}
		if normalized[i] == "client" {
			clientAt = i
			break
		}
	}
	translateEnd = min(translateEnd, clientAt)
	for i := 1; i < translateEnd; i++ {
		if flagValues[i] {
			continue
		}
		switch {
		case normalized[i] == "--noenv":
			normalized[i] = "--no-client-env"
		case strings.HasPrefix(normalized[i], "--noenv="):
			normalized[i] = "--no-client-env=" + strings.TrimPrefix(normalized[i], "--noenv=")
		}
	}
	return normalized
}

func rootBoolFlag(arg string) bool {
	switch arg {
	case "--keyseed-ec", "--noenv", "--no-client-env", "-n", "--no-inherit-env", "--no-global-env",
		"--verbose", "-v", "--quiet", "-q", "--sftp", "-s", "--sftp-wd",
		"--tcp-forwarding", "-t", "--mosh", "--version", "--help":
		return true
	default:
		return false
	}
}

func getEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}
