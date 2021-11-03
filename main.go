package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"log/syslog"

	gosshpot "github.com/gbroiles/gosshpot/server"
)

var version string = "0.0.0-src" //set via ldflags

var help = `
  Usage: gosshpot [options] 

  Version: ` + version + `

  Options:
    --host, listening interface (defaults to all)
    --port -p, listening port (defaults to 22, then fallsback to 2200)
    --shell, the type of to use shell for remote sessions (defaults to $SHELL, then bash/powershell)
    --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
    --keyseed, a string to use to seed key generation
    --noenv, ignore environment variables provided by the client
    --version, display version
    -v, verbose logs

  Notes:
    * if no keyfile and no keyseed are set, a random RSA2048 key is used

  Read more: https://github.com/gbroiles/gosshpot

`

func main() {

    syslogger, err := syslog.New(syslog.LOG_INFO, "gosshpot")
    if err != nil {
        log.Fatalln(err)
    }

    log.SetOutput(syslogger)

	flag.Usage = func() {
		fmt.Print(help)
		os.Exit(1)
	}

	//init config from flags
	c := &gosshpot.Config{}
	flag.StringVar(&c.Host, "host", "0.0.0.0", "")
	flag.StringVar(&c.Port, "p", "", "")
	flag.StringVar(&c.Port, "port", "", "")
	flag.StringVar(&c.Shell, "shell", os.Getenv("SHELL"), "")
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

	s, err := gosshpot.NewServer(c)
	if err != nil {
		log.Fatal(err)
	}
	err = s.Start()
	if err != nil {
		log.Fatal(err)
	}
}
