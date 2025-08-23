package main

import (
	"github.com/jpillora/opts"
	"github.com/jpillora/sshd-lite/smux"
)

type config struct {
	Daemon daemonConfig `opts:"mode=cmd,help=Run SSH daemon on Unix socket"`
	Attach attachConfig `opts:"mode=cmd,help=Attach to a shell session"`
	List   listConfig   `opts:"mode=cmd,help=List active shell sessions"`
	New    newConfig    `opts:"mode=cmd,help=Create a new bash session"`
}

type daemonConfig struct {
	Foreground bool `opts:"help=run in foreground mode"`
}

func (d *daemonConfig) Run() error {
	if d.Foreground {
		return smux.RunDaemonProcess(true)
	} else {
		return smux.StartDaemonBackground()
	}
}

type attachConfig struct {
	Session string `opts:"help=session name to attach to"`
}

func (a *attachConfig) Run() error {
	return smux.AttachToSession(a.Session)
}

type listConfig struct{}

func (l *listConfig) Run() error {
	return smux.ListSessions()
}

type newConfig struct {
	Name    string `opts:"help=session name (optional)"`
	Command string `opts:"help=initial command to run (optional)"`
}

func (n *newConfig) Run() error {
	return smux.CreateNewSession(n.Name, n.Command)
}

func main() {
	c := config{}
	opts.Parse(&c).Run()
}
