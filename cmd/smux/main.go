package main

import (
	"fmt"
	"os"
	"os/user"

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
	Background bool `opts:"help=run in background mode"`
	Stop       bool `opts:"help=stop the running daemon"`
	Restart    bool `opts:"help=restart the daemon"`
}

func (d *daemonConfig) Run() error {
	daemon := smux.NewDaemon(d.Config)
	
	if d.Stop {
		return daemon.Stop()
	}
	
	if d.Restart {
		if err := daemon.Stop(); err != nil {
			fmt.Printf("Warning: failed to stop daemon: %v\n", err)
		}
	}
	
	if daemon.IsRunning() {
		fmt.Println("already running")
		return nil
	}
	if d.Background {
		return daemon.StartBackground()
	}
	// Check if we're nested (spawned by background daemon)
	isNested := os.Getenv("SMUX_NESTED") == "1"
	return daemon.Run(!isNested)
}

type attachConfig struct {
	smux.Config
	Target  string `opts:"help=connection target"`
	Session string `opts:"help=session name to attach to"`
}

func (a *attachConfig) Run() error {
	// Set defaults
	if a.Target == "" {
		if a.Config.SocketPath == "" {
			a.Config.SocketPath = smux.GetDefaultSocketPath()
		}
		a.Target = "unix://" + a.Config.SocketPath
	}
	
	if a.Session == "" {
		if currentUser, err := user.Current(); err == nil {
			a.Session = currentUser.Username
		} else {
			a.Session = "default"
		}
	}
	
	client := smux.NewClient(a.Config)
	return client.AttachToSessionSSH(a.Target, a.Session)
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
