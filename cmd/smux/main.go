package main

import (
	"fmt"
	"os"

	"github.com/jpillora/opts"
	"github.com/jpillora/sshd-lite/pkg/smux"
)

type config struct {
	Daemon daemonConfig `opts:"mode=cmd,help=Run SSH daemon on Unix socket"`
	Attach attachConfig `opts:"mode=cmd,help=Attach to a shell session"`
	List   listConfig   `opts:"mode=cmd,help=List active shell sessions"`
	New    newConfig    `opts:"mode=cmd,help=Create a new bash session"`
}

type daemonConfig struct {
	smux.Config
	Background   bool `opts:"help=run in background mode"`
	NoForeground bool `opts:"help=run in background mode (internal use)" name:"no-foreground"`
}

func (d *daemonConfig) Run() error {
	daemon := smux.NewDaemon(d.Config)
	if daemon.IsRunning() {
		fmt.Println("already running")
		return nil
	}
	if d.Background {
		return daemon.StartBackground()
	}
	// If NoForeground is set, run in background mode (with logging)
	return daemon.Run(!d.NoForeground)
}

type attachConfig struct {
	smux.Config
	Session string `opts:"help=session name to attach to"`
}

func (a *attachConfig) Run() error {
	client := smux.NewClient(a.Config)
	return client.AttachToSession(a.Session)
}

type listConfig struct {
	smux.Config
}

func (l *listConfig) Run() error {
	client := smux.NewClient(l.Config)
	return client.ListSessions()
}

type newConfig struct {
	smux.Config
	Name    string `opts:"help=session name (optional)"`
	Command string `opts:"help=initial command to run (optional)"`
}

func (n *newConfig) Run() error {
	client := smux.NewClient(n.Config)
	return client.CreateNewSession(n.Name, n.Command)
}

func main() {
	c := config{}
	o := opts.New(&c).Name("smux").Parse()
	if !o.IsRunnable() {
		println(o.Help())
		os.Exit(0)
	}
	o.RunFatal()
}
