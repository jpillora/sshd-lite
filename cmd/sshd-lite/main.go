package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	sshd "github.com/jpillora/sshd-lite/pkg/server"
)

var version string = "0.0.0-src" //set via ldflags

var help = `
  Usage: sshd-lite [options] <auth>

  Version: ` + version + `

  Options:
    --host, listening interface (defaults to all)
    --port -p, listening port (defaults to 22, then fallsback to 2200)
    --shell, the type of to use shell for remote sessions (defaults to $SHELL, then bash/powershell)
    --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
    --keyseed, a string to use to seed key generation
    --noenv, ignore environment variables provided by the client
    --keepalive, server keep alive interval seconds (defaults to 60, 0 to disable)
    --sftp -s, enable the SFTP subsystem (disabled by default)
    --tcp-forwarding -t, enable TCP forwarding (both local and reverse, disabled by default)
    --version, display version
    --verbose -v, verbose logs

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

  Read more: https://github.com/jpillora/sshd-lite

`

func main() {

	flag.Usage = func() {
		fmt.Print(help)
		os.Exit(1)
	}

	//init config from flags
	c := &sshd.Config{}
	flag.StringVar(&c.Host, "host", "0.0.0.0", "")
	flag.StringVar(&c.Port, "p", "", "")
	flag.StringVar(&c.Port, "port", "", "")
	flag.StringVar(&c.Shell, "shell", os.Getenv("SHELL"), "")
	flag.StringVar(&c.KeyFile, "keyfile", "", "")
	flag.StringVar(&c.KeySeed, "keyseed", "", "")
	flag.IntVar(&c.KeepAlive, "keepalive", 60, "")
	flag.BoolVar(&c.IgnoreEnv, "noenv", false, "")
	flag.BoolVar(&c.SFTP, "s", false, "")
	flag.BoolVar(&c.SFTP, "sftp", false, "")
	flag.BoolVar(&c.TCPForwarding, "t", false, "")
	flag.BoolVar(&c.TCPForwarding, "tcp-forwarding", false, "")

	//help/version
	h1f := flag.Bool("h", false, "")
	h2f := flag.Bool("help", false, "")
	v1f := flag.Bool("verbose", false, "")
	v2f := flag.Bool("v", false, "")
	vf := flag.Bool("version", false, "")
	flag.Parse()

	if *vf {
		fmt.Print(version)
		os.Exit(0)
	}
	if *h1f || *h2f {
		flag.Usage()
	}

	c.LogVerbose = *v1f || *v2f

	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
	}
	c.AuthType = args[0]

	s, err := sshd.NewServer(c)
	if err != nil {
		log.Fatal(err)
	}
	err = s.Start()
	if err != nil {
		log.Fatal(err)
	}
}
