package main

import (
	"github.com/jpillora/opts"
)

type config struct {
	Daemon daemonConfig `opts:"mode=cmd,help=Run SSH daemon on Unix socket"`
	Attach attachConfig `opts:"mode=cmd,help=Attach to a shell session"`
	List   listConfig   `opts:"mode=cmd,help=List active shell sessions"`
}

type daemonConfig struct {
	Foreground bool `opts:"help=run in foreground mode"`
}

func (d *daemonConfig) Run() error {
	if d.Foreground {
		return runDaemonProcess(true)
	} else {
		return startDaemonBackground()
	}
}

type attachConfig struct {
	Session string `opts:"help=session name to attach to"`
}

func (a *attachConfig) Run() error {
	return runAttachCommand(a.Session)
}

type listConfig struct{}

func (l *listConfig) Run() error {
	return runListCommand()
}

func main() {
	c := config{}
	opts.Parse(&c).Run()
}
