//go:generate go tool github.com/jpillora/md-tmpl -w README.md

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jpillora/opts"
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
* remote commands stream stdin, stdout, and stderr and report their exit status
* shells, commands, and SFTP start in workdir; if unset, it is the process
  working directory when the server is created
* handshake-timeout and max-pending-handshakes use safe defaults when zero;
  set either to a negative value to disable that protection
`

func main() {
	c := sshd.Config{
		Host:                 "0.0.0.0",
		KeepAlive:            60,
		HandshakeTimeout:     sshd.DefaultHandshakeTimeout,
		MaxPendingHandshakes: sshd.DefaultMaxPendingHandshakes,
	}

	opts.New(&c).
		Name("sshd-lite").
		Version(version).
		Repo("github.com/jpillora/sshd-lite").
		PkgRepo().
		DocAfter("args", "authArg", authArg).
		DocBefore("version", "notes", notes).
		Parse()

	s, err := sshd.NewServer(c)
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
