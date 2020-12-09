package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	sshd "github.com/jpillora/sshd-lite/server"
)

var version string = "0.0.0-src" //set via ldflags

var help = `
  Usage: sshd-lite [options] <auth>

  Version: ` + version + `

  Options:
    --host, listening interface (defaults to all)
    --port -p, listening port (defaults to 22, then fallsback to 2200)
    --shell, the type of to use shell for remote sessions (defaults to bash)
    --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
    --keyseed, a string to use to seed key generation
    --noenv, ignore environment variables provided by the client
    --version, display version
    -v, verbose logs

  <auth> must be set to one of:
    1. a username and password string separated by a colon ("user:pass")
    2. a path to an ssh authorized keys file ("~/.ssh/authorized_keys")
    3. "none" to disable client authentication :WARNING: very insecure

  Notes:
    * if no keyfile and no keyseed are set, a random RSA2048 key is used
    * once authenticated, clients will have access to a shell of the
    current user. sshd-lite does not lookup system users.
    * sshd-lite only supports remotes shells. tunnelling and command
    execution are not currently supported.

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
	flag.StringVar(&c.Shell, "shell", "", "")
	flag.StringVar(&c.KeyFile, "keyfile", "", "")
	flag.StringVar(&c.KeySeed, "keyseed", "", "")
	flag.BoolVar(&c.IgnoreEnv, "noenv", false, "")
	flag.BoolVar(&c.LogVerbose, "v", false, "")

	//help/version
	h1f := flag.Bool("h", false, "")
	h2f := flag.Bool("help", false, "")
	vf := flag.Bool("version", false, "")
	flag.Parse()

	if *vf {
		fmt.Print(version)
		os.Exit(0)
	}
	if *h1f || *h2f {
		flag.Usage()
	}

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
