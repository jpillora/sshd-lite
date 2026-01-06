//go:generate go tool github.com/jpillora/md-tmpl -w README.md

package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/jpillora/opts"
	"github.com/jpillora/sshd-lite/sshd"
)

var version string = "0.0.0-src" //set via ldflags

const notes = `
<auth> must be set to one of:
1. a username and password string separated by a colon ("myuser:mypass")
2. a path to an ssh authorized keys file ("~/.ssh/authorized_keys")
3. an authorized github user ("github.com/myuser") public keys from .keys
4. "none" to disable client authentication :WARNING: very insecure

Notes:
* if no keyfile and no keyseed are set, a random RSA2048 key is used
* authorized_key files are automatically reloaded on change
* once authenticated, clients will have access to a shell of the
  current user. sshd-lite does not lookup system users.
* sshd-lite only supports remotes shells, sftp, and tcp forwarding. command
  execution are not currently supported.
* sftp working directory is the home directory of the user
`

func main() {
	c := &sshd.Config{
		Host:      "0.0.0.0",
		KeepAlive: 60,
	}

	opts.New(c).
		Name("sshd-lite").
		Version(version).
		Repo("github.com/jpillora/sshd-lite").
		PkgRepo().
		DocAfter("args", "notes", notes).
		Parse()

	s, err := sshd.NewServer(c)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	err = s.StartContext(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
