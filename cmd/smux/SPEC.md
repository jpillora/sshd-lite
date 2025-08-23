# `smux` ssh-terminal-multiplexer

* cli tool uses http://github.com/jpillora/opts (see opts README.md)
* cli tool has these sub-commands:

* `smux daemon`
    * runs in foreground or background
        * default is background, use `--foreground` flag to run in foreground
    * writes `/var/run/smux.pid` on start
    * in foreground mode, writes stdout to stdout
    * in background mode, writes stdout to `/var/run/smux.log`
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

## user workflow

1. user attaches to a named shell
1. `smux attach` doesnt detect a running `daemon`
1. so runs `smux daemon`, which is a normal ssh server listening on a unix socket
1. `smux attach` should now detect a running `daemon`
1. `smux attach` connects to the pty and replaces the current shell with the remote shell