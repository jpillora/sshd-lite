# `smux` ssh-terminal-multiplexer

* cli tool uses http://github.com/jpillora/opts (see opts README.md)
* cli tool has these sub-commands:

1. `smux daemon` runs the daemon in foreground
    * runs in foreground by default
    * writes a pid file on start to `/var/run/smux.pid` (default location)
    * writes logs to stdout
    * with `smux daemon --background` set, runs the daemon in background
        * forks current process (with setsid)
        * passes `--pidfile` flag to child process
        * sets `--logfile` flag to child process (default `/var/log/smux.log`)
    * `daemon` is an ssh server (uses the `server` package)
        * note: `server` implements a light version of normal `sshd` server
    * by default, only listens on `/var/run/smux.sock`
    * all shells use `bash`
* `smux attach`
    * `attach` is an ssh client (needs a new `client` package)
    * if `daemon` not running, runs `daemon` in background
    * by default, connects to `/var/run/smux.sock`
* `smux list`
    * prints the running shells
    * if `daemon` not running, runs none 
    * implemented as an ssh request
        * request "list" with want-reply:true
        * response is a JSON payload

## file structure

* `cmd/smux/main.go` should be minimal, just call `opts.Parse` and `opts.Run`
* the commands should be minimal too, they should:
    * parse a `daemon.Config` (may contain `opts` struct tags)
    * call a `daemon.Run(config)` function
    * the daemon command in `cmd/` should have `--background` flag, but it should not be in `daemon.Config`. the `--background` flag should be handled in `cmd/smux/daemon.go` via struct embedding (see opts docs)

## user workflow

1. user attaches to a named shell
1. `smux attach` doesnt detect a running `daemon`
1. so runs `smux daemon`, which is a normal ssh server listening on a unix socket
1. `smux attach` should now detect a running `daemon`
1. `smux attach` connects to the pty and replaces the current shell with the remote shell