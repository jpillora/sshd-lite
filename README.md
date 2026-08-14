# sshd-lite

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=00add8)](https://pkg.go.dev/github.com/jpillora/sshd-lite)
[![CI](https://github.com/jpillora/sshd-lite/workflows/CI/badge.svg)](https://github.com/jpillora/sshd-lite/actions?workflow=CI)

A feature-light Secure Shell Daemon `sshd(8)` written in Go (Golang). A slightly more practical version of the SSH daemon described in [this Gopher Academy post](https://blog.gopheracademy.com/go-and-ssh/).

### Install

**Binaries**

See [the latest release](https://github.com/jpillora/sshd-lite/releases/latest)

One-line download and install

```sh
curl https://i.jpillora.com/sshd-lite! | bash
```

**Source**

``` sh
go install github.com/jpillora/sshd-lite@latest
```

### Features

* Cross platform binaries with no dependencies
* Interactive shells (`bash` on Linux/macOS and `powershell` on Windows)
* Remote command execution with stdin, separate stdout/stderr, and exit status
* Authentication (`user:pass`, an unrestricted `authorized_keys` file, `github.com/foobar`, or `none`)
* Seed server-key generation
* Enable SFTP support with `--sftp` (allows `scp` and other SFTP clients)
* Enable TCP forwarding with `--tcp-forwarding` (both local and reverse forwarding)

### Quick use

Server

``` sh
$ curl https://i.jpillora.com/sshd-lite! | sh
Downloading: sshd-lite_1.1.0_darwin_amd64
######################################### 100.0%
$ sshd-lite john:doe
2020/12/09 23:55:08 Key from system rng
2020/12/09 23:55:08 RSA key fingerprint is SHA256:kLK6RD2tCqSfvYxdMPa3YRNwUJS09njfE1hXoqOYXG4.
2020/12/09 23:55:08 Authentication enabled (user 'john')
2020/12/09 23:55:08 Listening on 0.0.0.0:2200...
```

Client

```sh
$ ssh john@localhost -p 2200
The authenticity of host '[localhost]:2200 ([::1]:2200)' can't be established.
RSA key fingerprint is SHA256:kLK6RD2tCqSfvYxdMPa3YRNwUJS09njfE1hXoqOYXG4.
Are you sure you want to continue connecting (yes/no/[fingerprint])? yes # note fingerprint matches
john@localhost's password: *** # enter password from above
bash-3.2$ date
Wed  9 Dec 2020 23:57:22 AEDT
```

### Usage

```
$ sshd-lite --help
```

<!-- regenerate help with: go generate ./... -->
<!--tmpl,code=plain:echo "$ sshd-lite --help" && go run main.go --help 2>&1 | sed 's#0.0.0-src#X.Y.Z#' -->
``` plain 
$ sshd-lite --help

  Usage: sshd-lite [options] <auth>

  <auth> must be set to one of:
  1. a username and password string separated by a colon ("myuser:mypass")
  2. a path to an ssh authorized_keys file ("~/.ssh/authorized_keys"); entries
     must be unrestricted, and any entry with options makes the file invalid
  3. a GitHub user ("github.com/myuser"); public keys are fetched from .keys
  4. "none" to disable client authentication :WARNING: very insecure

  Options:
  --host, -h                    listening interface (defaults to all, default 0.0.0.0)
  --port, -p                    listening port (defaults to 22 then fallsback to 2200)
  --shell                       the shell to use for remote sessions (default bash/powershell, env
                                SHELL)
  --workdir, -w                 working directory for sessions (default current directory)
  --keyfile, -k                 a filepath to a private key (for example an 'id_rsa' file)
  --keyseed                     a string to use to seed key generation (env KEYSEED)
  --keyseed-ec                  use elliptic curve for key generation (env KEYSEED_EC)
  --keepalive                   server keep alive interval seconds (0 to disable, default 60)
  --handshake-timeout           maximum time for an SSH handshake (negative to disable, default
                                10s)
  --max-pending-handshakes, -m  maximum concurrent unauthenticated SSH handshakes (negative to
                                disable, default 64)
  --noenv, -n                   ignore environment variables provided by the client
  --inherit-env, -i             give sessions the server process's own environment (may expose
                                secrets held by whoever started sshd-lite)
  --verbose, -v                 verbose logs
  --quiet, -q                   no logs
  --sftp, -s                    enable the SFTP subsystem (disabled by default)
  --tcp-forwarding, -t          enable TCP forwarding (both local and reverse; disabled by default)
  --version                     display version
  --help                        display help

  Notes:
  * if no keyfile and no keyseed are set, a random 2048-bit RSA key is used
  * authorized_keys files are validated at startup and reloaded for every public
    key authentication; a failed reload denies authentication until the file is
    valid again, and any entry with options makes the entire file invalid
  * authenticated names do not select system users or change privileges; shells
    and commands run as the user that started sshd-lite
  * remote commands stream stdin, stdout, and stderr and report their exit status
  * shells, commands, and SFTP start in workdir; if unset, it is the process
    working directory when the server is created
  * handshake-timeout and max-pending-handshakes use safe defaults when zero;
    set either to a negative value to disable that protection

  Version:
    X.Y.Z

  Read more:
    github.com/jpillora/sshd-lite

```
<!--/tmpl-->

### Programmatic Usage

Use the [`sshd` package](https://pkg.go.dev/github.com/jpillora/sshd-lite/sshd) to configure and run a server. Cancelling the context stops the listener, closes active SSH connections, and waits for connection-owned protocol handlers to finish.

```go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/jpillora/sshd-lite/sshd"
)

func main() {
	server, err := sshd.NewServer(sshd.Config{
		Host:          "127.0.0.1",
		Port:          "2222",
		KeyFile:       "/path/to/ssh_host_key",
		AuthType:      "/path/to/authorized_keys",
		WorkDir:       "/srv/ssh",
		SFTP:          true,
		TCPForwarding: false,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := server.StartContext(ctx); err != nil {
		log.Fatal(err)
	}
}
```

`WorkDir` is shared by shells, remote commands, and SFTP. If it is empty, `NewServer` uses the process working directory. SFTP and TCP forwarding are disabled unless enabled in `Config`. Zero `HandshakeTimeout` and `MaxPendingHandshakes` values select the safe defaults (10 seconds and 64); negative values disable the corresponding protection. `StartWithContext` accepts an existing `net.Listener` when the caller needs to control address selection.

For in-memory public-key authentication, set `AuthKeys` and leave `AuthType` empty. `AuthKeys` accepts bare `ssh.PublicKey` values only: keys are not tied to login names, and `authorized_keys` options or per-key restrictions cannot be expressed. File-based authentication validates the file at startup, reloads it for each public-key authentication, and denies authentication whenever a reload cannot produce a valid unrestricted key set. Any parsed entry with `authorized_keys` options invalidates the whole file.

Custom handler maps cannot reuse names reserved by enabled shell/session, SFTP, or forwarding handlers; `NewServer` reports those conflicts as configuration errors. The older [`server` package](https://pkg.go.dev/github.com/jpillora/sshd-lite/server) remains only as a deprecated compatibility layer; new code should use `sshd` and, for lower-level protocol handling, [`xssh`](https://pkg.go.dev/github.com/jpillora/sshd-lite/xssh).

#### MIT License

Copyright © 2026 Jaime Pillora &lt;dev@jpillora.com&gt;

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
'Software'), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED 'AS IS', WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
