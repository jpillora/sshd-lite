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
* Built-in SSH client: `sshd-lite client user@host`
* Mosh terminal sessions over UDP: enable `--mosh` on both server and client

Sessions inherit the server process's environment by default. Set
`--no-inherit-env` (`Config.NoInheritEnv`) to inherit only essential shell
variables. On Unix, `/etc/environment` supplies defaults when present, including
with inheritance disabled; `--no-global-env` (`Config.NoGlobalEnv`) disables
loading this file. Inherited process values take precedence. Its
`KEY=value` entries support quotes and comments, with no shell execution or
variable expansion. Client environment requests and PTY `TERM` override those
values; `--noenv` separately ignores client environment requests.

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

### Built-in SSH and Mosh client

```sh
# Server: TCP and UDP both listen on port 2222
sshd-lite --mosh --port 2222 user:pass

# SSH shell, or a command with streamed stdin/stdout/stderr and exit status
sshd-lite client --port 2222 user@host
sshd-lite client --port 2222 user@host 'uname -a'

# Mosh shell: authenticate over SSH, then communicate over UDP
sshd-lite client --mosh --port 2222 user@host
```

The client checks `~/.ssh/known_hosts`; use `--known-hosts` to select another
file. For a local test with a randomly generated server host key, explicitly
add `--insecure`. Authentication uses the SSH agent, default Ed25519/RSA keys,
`--identity`, or a password prompt (`--password` also works).

Mosh uses [mosh-go](https://github.com/unixshells/mosh-go) encryption and wire
codecs, with a local state synchronization transport and terminal renderer. Open **both TCP
and UDP** on the chosen port. All sessions share that UDP port, including when
the server chooses port 2200 as its fallback. Each authenticated SSH request
issues a fresh 128-bit session key. The SSH connection then closes; changing
UDP source addresses does not end the shell.

A key expires **five minutes after issuance or the last fresh, authenticated
UDP packet**, whichever is later. Encrypted UDP keepalives run automatically
in both directions (at the protocol's adaptive retransmission interval,
250 ms–10 s). They keep an idle connected terminal alive. A disconnected
session expires after five minutes; reconnect with a new SSH handshake and
key after expiry. Replays, invalid packets and server output alone cannot
extend the timeout. At most 64 pending or active Mosh sessions are allowed.

Mosh starts the same configured shell in the same working directory and uses
the same environment policy as SSH. Exit the shell normally, or type
**Ctrl-^ then .** to disconnect. Terminal resizing is supported on Unix;
Windows retains its initial ConPTY size, matching the existing backend's
limitation. Mosh can also launch a terminal command; use SSH for separate
stderr, binary streams, or portable remote exit-status reporting.

The client and server interoperate independently with Debian Mosh 1.4.0:

```sh
# Standard Debian client → sshd-lite server (server started with --mosh)
mosh --ssh='ssh -p 2222' user@host

# sshd-lite client → standard Mosh server via SSH
sshd-lite client --mosh user@host

# Run a terminal command; arguments are passed literally
sshd-lite client --mosh --port 2222 user@host tmux new-session
```

With `--mosh`, sshd-lite handles literal `mosh-server new ...` commands inside
an authenticated SSH exec channel and returns the standard `MOSH CONNECT`
response. No external server executable is needed. Ordinary SSH commands still
use the configured shell. The client uses this standard bootstrap and accepts
`--mosh-server /path/to/mosh-server` for an alternate remote executable.

For remote commands containing options, put `--` before the destination, for
example `sshd-lite client --mosh -- user@host tmux new-session -s work`.

The embedded server always uses its shared UDP port: a requested `-p` range
must include that port. It supports the standard launcher's proxy and remote
address discovery. For multihomed servers, bind sshd-lite to the intended local
address; the shared socket does not select a source address per session.
Custom shell wrappers/expansions and absolute executable paths are executed
by the ordinary SSH shell, outside the virtual bootstrap.

Standard Mosh has no remote process exit-status field; mixed sessions return
success on clean protocol shutdown. Two sshd-lite peers negotiate an optional
exit-status extension. Mosh synchronizes the current screen, so a connection resuming
within the five-minute grace period restores it but does not recover scrollback.
The simple client has no predictive local echo. Terminal behavior is limited to
the emulator's supported escape sequences; this is not a claim of complete
terminal-feature parity with Debian Mosh.

Linux interoperability tests use installed `mosh`, `mosh-client`, `mosh-server`,
`sshpass`, `tmux`, and `/usr/sbin/sshd`. Set `MOSH_TEST_BIN` to test another Mosh
installation, `MOSH_TEST_LITE` to a built sshd-lite binary, and
`MOSH_TEST_REQUIRED=1` to fail when a required test dependency is missing.

Run `sshd-lite client --help` for client options.

### Embedding Mosh in Go

Both peers are public Go APIs. The CLI uses the same client session implementation.
Importing `sshd`, `client`, or `xssh` alone does not import Mosh, its terminal
emulator, or its bootstrap parser. The CLI opts in to Mosh when `--mosh` is set.
The module remains a single Go module; the default CLI binary includes both
protocols, while SSH-only library builds exclude the Mosh packages.
See [architecture](docs/architecture.md) for package ownership and dependencies.
See [example/mosh/main.go](example/mosh/main.go) for a complete in-process server
and client with generated keys, verified authentication, and context shutdown:

```sh
go run ./example/mosh
```

Import `github.com/jpillora/sshd-lite/mosh` explicitly to enable the embedded
server:

```go
server, err := sshd.NewServer(sshd.Config{
    AuthKeys: userKeys,
    KeyBytes: hostKeyPEM,
    Attach: mosh.Attach,
})
```

Then call `server.StartContext(ctx)` or
`server.StartWithContext(ctx, listener)`. A caller-supplied TCP listener also
selects the UDP port, including when bound to port zero. Cancelling the context
stops both listeners and their sessions. Configure `KeyBytes` and `AuthKeys`
for in-memory keys, or use the existing server authentication options.

Use the same opt-in `mosh` package for the client:

```go
// ctx, address, userKey, hostKey and screenWriter belong to your application.
session, err := mosh.Dial(ctx, address, &ssh.ClientConfig{
    User: "user",
    Auth: []ssh.AuthMethod{ssh.PublicKeys(userKey)},
    HostKeyCallback: ssh.FixedHostKey(hostKey),
}, mosh.ClientConfig{
    Columns: 80, Rows: 24,
    Output: screenWriter,
    // Command: []string{"tmux", "new-session"}, // optional literal argv
})
if err != nil {
    return err
}
defer session.Close()
if _, err := io.WriteString(session, "echo hello\n"); err != nil {
    return err
}
if err := session.Resize(100, 30); err != nil {
    return err
}
// Wait for remote exit, context cancellation, or Close from another goroutine.
code, err := session.Wait()
```

`mosh.Start(ctx, sshConn, config)` accepts an existing dedicated authenticated
`*ssh.Client`. It takes ownership and closes that SSH connection before returning,
including on failure; the resulting terminal uses UDP. `mosh.Dial` also closes
its bootstrap SSH connection. Supply authentication and host-key verification
explicitly through `ssh.ClientConfig`.

`mosh.Session` provides concurrent-safe `Write`, `Resize`, `Done`, `Wait`, and
idempotent `Close`. Writes copy and queue input with bounded backpressure;
resizes are asynchronous and coalesce. The library does not inspect stdin,
change local terminal modes, register signal handlers, or interpret the CLI's
Ctrl-^ `.` escape. Send unmodified arrows using application-mode SS3 sequences.
Mosh has no stdin EOF operation; an empty write does not close remote input.

`Output` receives ANSI screen updates through an `io.Writer`; nil discards them.
The writer must return promptly, and the application must unblock a blocked
writer before waiting for shutdown. The library does not close the writer.
Use a synchronized writer for concurrent reads, or inspect captured output after
`Done` or `Wait`. Standard Mosh peers return zero on clean shutdown; two Lite
peers also transmit the remote exit status. Cancellation returns the context
error, and `Close` allows up to three seconds for protocol shutdown.

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
  --no-inherit-env              inherit only essential shell variables from the server process
  --no-global-env               do not load /etc/environment for sessions
  --verbose, -v                 verbose logs
  --quiet, -q                   no logs
  --sftp, -s                    enable the SFTP subsystem (disabled by default)
  --tcp-forwarding, -t          enable TCP forwarding (both local and reverse; disabled by default)
  --mosh                        enable Mosh on the same UDP port (five-minute idle timeout)
  --version                     display version
  --help                        display help

  Notes:
  * if no keyfile and no keyseed are set, a random 2048-bit RSA key is used
  * authorized_keys files are validated at startup and reloaded for every public
    key authentication; a failed reload denies authentication until the file is
    valid again, and any entry with options makes the entire file invalid
  * authenticated names do not select system users or change privileges; shells
    and commands run as the user that started sshd-lite
  * sessions inherit the process environment; no-inherit-env limits this to
    essential shell variables; /etc/environment supplies defaults on Unix
    unless no-global-env is set
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
