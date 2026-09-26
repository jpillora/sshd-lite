# Mosh guide

sshd-lite includes an optional Mosh-compatible terminal transport. SSH handles
authentication and bootstrap; the interactive terminal then communicates over
encrypted UDP. The server and client interoperate independently with standard
Mosh, including Debian Mosh 1.4.0.

For implementation boundaries and protocol verification, see
[architecture](architecture.md) and
[Mosh compatibility and verification](mosh-compatibility.md).

## Quick start

Open TCP and UDP on the same port, then enable Mosh on both peers:

```sh
# Server: TCP and UDP both listen on port 2222
sshd-lite --mosh --port 2222 user:pass

# sshd-lite client
sshd-lite client --mosh --port 2222 user@host
```

All sessions share the server's SSH port for UDP, including port 2200 when the
server falls back from port 22. Normal SSH authentication and host-key
verification apply to the bootstrap connection. Use `--known-hosts` to select a
file. By default, new keys are saved without prompting in
`$XDG_STATE_HOME/sshd-lite/known_hosts` (falling back to
`~/.local/state/sshd-lite/known_hosts`), while changed keys still require
confirmation. Use `--strict-hosts` to confirm new keys too,
`--share-known-hosts` to use `~/.ssh/known_hosts`, `--accept` to replace a
changed key non-interactively, or `--insecure` only when verification is
intentionally disabled.

An sshd-lite server started without `--mosh` promptly rejects the standard Mosh
bootstrap instead of leaving the client waiting.

## Standard Mosh interoperability

Either side can be replaced by a standard Mosh peer:

```sh
# Standard client to sshd-lite server
mosh --ssh='ssh -p 2222' user@host

# sshd-lite client to a standard Mosh server through SSH
sshd-lite client --mosh user@host

# Select another remote mosh-server executable
sshd-lite client --mosh --mosh-server /path/to/mosh-server user@host
```

With `--mosh`, sshd-lite handles the standard `mosh-server new ...` bootstrap
inside an authenticated SSH exec channel and returns `MOSH CONNECT`. It does not
need an external `mosh-server` executable. Ordinary SSH commands continue to use
the configured shell.

The embedded server supports the standard launcher's proxy and remote-address
discovery. A requested UDP port or range must include sshd-lite's shared port.
On a multihomed server, bind sshd-lite to the intended local address because the
shared socket does not select a source address per session.

## Terminal commands

Mosh normally starts the configured shell in the configured work directory,
using the same environment policy as SSH. It can instead launch a terminal
command with literal arguments:

```sh
sshd-lite client --mosh --port 2222 user@host tmux new-session
```

If the remote command contains options that could be parsed locally, put `--`
before the destination:

```sh
sshd-lite client --mosh --port 2222 -- user@host tmux new-session -s work
```

Use ordinary SSH rather than Mosh when separate stderr, binary streams, or
portable remote exit-status reporting are required. Standard Mosh has no remote
process exit-status field, so mixed sessions return success after clean protocol
shutdown. Two sshd-lite peers negotiate an optional exit-status extension.

## Session lifetime and network behavior

Each authenticated SSH bootstrap issues a fresh 128-bit session key, then closes
the SSH connection. An authenticated UDP source-address change can move the
session, allowing it to survive common network changes.

A key expires five minutes after issuance or the last fresh, authenticated UDP
packet, whichever is later. Encrypted keepalives run automatically in both
directions at the adaptive protocol retransmission interval of 250 milliseconds
to 10 seconds. They keep an idle connected terminal alive. A disconnected
session expires after five minutes and then requires a new SSH bootstrap.

Replayed or invalid packets and server output alone do not extend the timeout.
The server accepts at most 64 pending or active Mosh sessions.

### Load-balancer routing prefix

The sshd-lite client can prepend an optional cleartext routing envelope to every
client-to-server UDP datagram:

```sh
sshd-lite client --mosh --mosh-prefix 16909060 user@host
```

Go clients set the same value with `mosh.ClientConfig.Prefix`.

The eight-byte envelope is `80 4d 50 01` followed by the configured `uint32` in
network byte order. A load balancer can route on the value and strip the eight
bytes before forwarding the original, fully standard Mosh datagram. Server
replies remain standard and carry no envelope. An sshd-lite Mosh server also
recognizes and silently removes the envelope, which makes direct connections
between sshd-lite peers work without a separate load balancer.

The value is an unauthenticated routing hint, not a session credential; Mosh's
encrypted packet authentication still decides whether the selected backend
accepts it. Zero disables the extension. A standard Mosh server requires the
load balancer to remove the envelope and otherwise rejects prefixed packets.

## Terminal behavior and limits

- Exit the shell normally, or type **Ctrl-^** followed by **.** to disconnect.
- Unix sessions support terminal resizing. Windows retains its initial ConPTY
  size because the existing backend does not support runtime resize.
- Mosh synchronizes the current screen, not scrollback. A temporary network
  outage can restore the screen while the same client and server session remain
  alive, but restarting the client does not reattach to a saved session.
- When an application leaves the alternate screen, the client preserves the
  caller's restored local primary screen instead of repainting the remote
  primary in that transition. One-shot detach/status text in the same update
  may therefore be omitted; later remote output continues normally.
- The simple client has no predictive local echo and is intentionally more
  conservative than the standard Mosh scheduler.
- Terminal behavior is limited to the emulator's supported escape sequences;
  the interoperability suite covers Unicode, colors, cursor modes, resizing,
  tmux, and terminal restoration, but is not exhaustive terminal conformance.
- Custom launcher shell wrappers and absolute executable paths run through the
  ordinary SSH shell rather than the embedded virtual bootstrap.

## Embedding the server

Mosh is opt-in for Go applications. Importing `sshd`, `client`, or `xssh` alone
does not import its terminal emulator or bootstrap parser.

Import `github.com/jpillora/sshd-lite/mosh` and attach it to the server:

```go
server, err := sshd.NewServer(sshd.Config{
	AuthKeys: userKeys,
	KeyBytes: hostKeyPEM,
	Attach:   mosh.Attach,
})
```

Call `server.StartContext(ctx)` or `server.StartWithContext(ctx, listener)`. A
caller-supplied TCP listener selects the UDP address and port, including when it
was bound to port zero. Cancelling the context stops both listeners and their
sessions.

The complete runnable example is in
[`example/mosh/main.go`](../example/mosh/main.go).

## Embedding the client

`mosh.Dial` creates its own SSH bootstrap connection from an explicit
`ssh.ClientConfig`:

```go
session, err := mosh.Dial(ctx, address, &ssh.ClientConfig{
	User:            "user",
	Auth:            []ssh.AuthMethod{ssh.PublicKeys(userKey)},
	HostKeyCallback: ssh.FixedHostKey(hostKey),
}, mosh.ClientConfig{
	Columns: 80,
	Rows:    24,
	Output:  screenWriter,
	// Command: []string{"tmux", "new-session"},
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
code, err := session.Wait()
```

`mosh.Start(ctx, sshConn, config)` instead accepts an existing, dedicated
authenticated `*ssh.Client`. Both functions take ownership of the bootstrap SSH
connection and close it before returning, including on failure. Supply
authentication and host-key verification explicitly through `ssh.ClientConfig`.

`mosh.Session` provides concurrent-safe `Write`, `Resize`, `Done`, `Wait`, and
idempotent `Close` methods. Writes copy and queue input with bounded
backpressure; resizes are asynchronous and coalesce. Mosh has no stdin EOF
operation, so an empty write does not close remote input.

The `Output` writer receives ANSI screen updates and must return promptly. The
library does not close it, inspect stdin, change local terminal modes, register
signals, or interpret the CLI's disconnect escape. Applications must unblock a
stalled writer before waiting for shutdown and synchronize concurrent access to
captured output themselves. Cancellation returns the context error; `Close`
allows up to three seconds for protocol shutdown.

## Interoperability tests

Linux integration tests use `mosh`, `mosh-client`, `mosh-server`, `sshpass`,
`tmux`, and `/usr/sbin/sshd`. Set `MOSH_TEST_BIN` to a Mosh installation,
`MOSH_TEST_LITE` to a built sshd-lite binary, and `MOSH_TEST_REQUIRED=1` to fail
when a required dependency is unavailable.

The detailed pairings, protocol coverage, findings, and platform results are
recorded in [Mosh compatibility and verification](mosh-compatibility.md).
