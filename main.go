//go:generate go tool github.com/jpillora/md-tmpl -w README.md

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jpillora/opts"
	"github.com/jpillora/sshd-lite/client"
	"github.com/jpillora/sshd-lite/mosh"
	"github.com/jpillora/sshd-lite/sshd"
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
* handshake-timeout and max-pending-handshakes use safe defaults when zero;
  set either to a negative value to disable that protection
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "client" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		c := struct {
			client.Config
			Mosh       bool   `opts:"name=mosh,help=use SSH to obtain a key then run the terminal over UDP"`
			MoshServer string `opts:"name=mosh-server,help=remote mosh-server executable (default mosh-server)"`
		}{}
		opts.New(&c).Name("sshd-lite client").Version(version).Parse()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		var code int
		var err error
		if c.Mosh {
			conn, connectErr := client.Connect(ctx, c.Config)
			err = connectErr
			if err == nil {
				code, err = mosh.Run(ctx, conn, os.Stdin, os.Stdout, mosh.ClientConfig{Server: c.MoshServer, Command: c.Command})
			}
		} else {
			code, err = client.Run(ctx, c.Config, os.Stdin, os.Stdout, os.Stderr)
		}
		cancel()
		if err != nil {
			log.Print(err)
			code = 1
		}
		os.Exit(code)
	}

	c := struct {
		sshd.Config
		Mosh bool `opts:"name=mosh,help=enable Mosh on the same UDP port (five-minute idle timeout)"`
	}{Config: sshd.Config{
		Host:                 "0.0.0.0",
		KeepAlive:            60,
		HandshakeTimeout:     sshd.DefaultHandshakeTimeout,
		MaxPendingHandshakes: sshd.DefaultMaxPendingHandshakes,
	}}

	opts.New(&c).
		Name("sshd-lite").
		Version(version).
		Repo("github.com/jpillora/sshd-lite").
		PkgRepo().
		DocAfter("args", "authArg", authArg).
		DocBefore("version", "notes", notes).
		Parse()

	if c.Mosh {
		c.Attach = mosh.Attach
	}
	s, err := sshd.NewServer(c.Config)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err = s.StartContext(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
