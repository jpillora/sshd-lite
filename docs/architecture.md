# Package ownership

The root command composes three public entry points:

- `sshd`: SSH server configuration, authentication, listener lifecycle and admission.
- `client`: SSH client configuration, authentication and local session adaptation.
- `mosh`: optional Mosh client API, interactive client, SSH bootstrap and listener attachment.

`xssh` supplies SSH connection/channel dispatch, shell and exec adaptation, SFTP,
and forwarding. It does not import Mosh. The deprecated `server` package retains
its compatibility facade.

```text
main
├── client ────────── internal/sshconn, internal/termio
├── sshd ──────────── xssh, sshd/key
└── mosh ──────────── xssh, internal/sshconn, internal/termio
    └── internal/mosh
        ├── ssp       encrypted datagrams and state synchronization
        └── display   emulator, immutable snapshots and rendering

xssh ─────────────── internal/terminal ── Unix PTY / vendored winpty
```

Shared dependencies form a directed graph: each package owns a cohesive area,
without forcing duplicate implementations into a strict tree. In particular:

- `internal/terminal` owns process startup, the single reaper, PTY release and
  resize serialization. It does not know either network protocol or emulate a
  screen. SSH owns channel closure, output draining and bounded shutdown;
  the detached terminal adapter owns its independent terminal lifetime.
- `internal/termio` owns local terminal modes and resize notifications.
- `internal/sshconn` owns context-aware TCP dialing and SSH handshake deadlines.
- `client.Dial` is the explicit-input connection boundary for library consumers;
  it does not read the process environment. `client.Connect` captures the existing
  CLI configuration and delegates connection establishment to that boundary.
  Callers own the returned upstream `ssh.Client` and its session lifetime.
- `github.com/jpillora/sftp` is the public, history-preserving SFTP fork used by
  both the server and test client. Its request server preserves open-handle
  FSTAT/FSETSTAT semantics after rename or unlink.
- `internal/mosh` owns UDP routing and session orchestration. `terminalIO` owns
  terminal pumps and response-pipe cancellation. Only the session loop mutates
  the display; it never reaches through the display into emulator internals.
- `internal/mosh/ssp` owns acknowledgements, replay tracking, retransmission,
  packet encoding/decoding and shutdown state. It does not import display or SSH.
- `internal/mosh/display` owns terminal emulation and screen reconstruction from
  accepted SSP updates. Its snapshots hide emulator-specific state.

# SSH/Mosh attachment

Embedded servers opt in with `sshd.Config{Attach: mosh.Attach}`. The complete
attachment boundary is one function:

```go
func(context.Context, net.Addr) (xssh.ExecHandler, io.Closer, error)
```

It runs once per SSH listener, before accepting connections. It receives only the
listener's context and address, returns the listener's virtual-exec handler, and
supplies a closer. The SSH server always closes that resource on attachment
failure or when its listener stops, including failure without context cancellation.
Concurrent listeners keep independent handler values and resource lifetimes.

`mosh.Attach` binds UDP on the same address and numeric port and handles the
standard `mosh-server new ...` exec command. Other commands fall through to SSH.
There is no service registry, naming, separate validation phase, or mutable SSH
configuration exposed to the hook. The proprietary JSON key request was removed.

The adapter uses `xssh.Session` and `xssh.StartTerminalCommand` directly. It copies
terminal launch policy and peer addresses during bootstrap; the deferred terminal
factory retains no SSH session, connection, or handler maps. The UDP session
starts its terminal on the first authenticated packet and owns it independently.

`mosh` imports `xssh` but neither `sshd` nor `client`. SSH-only imports still exclude
Mosh. Lower-level callers can call `mosh.Attach` themselves, install the returned
handler in `xssh.Config.ExecHandler`, and own its closer. The root command translates
`--mosh` into attachment/client selection.

The branch's unreleased Mosh API moved to `mosh.Dial`, `mosh.Start`,
`mosh.ClientConfig`, and `mosh.Session`. Session ownership, input, resize, output,
cancellation and exit-status contracts remain the same. Run the in-process example
with `go run ./example/mosh`.

`TestSSHDependenciesExcludeMosh` and `TestMoshDependencyBoundary` guard both
dependency directions. This is package dependency
isolation within one Go module, not a separate module or build-tag variant. The
default CLI supports both protocols and includes both implementations. When the
binary starts without `--mosh`, it installs a Mosh-owned rejecting attachment so
standard bootstrap commands fail promptly; SSH-only library users do not import
or install that optional handler.

# Test support

`sshd/sshtest` owns executable test actions, client resources and environment
lifecycle. Its client implementation is grouped into connection, options, shell,
session capture, SFTP and forwarding files. Actions and expectations use typed
`*Environment` internally; small adapters preserve their existing public interfaces.

`sshd/sshtest/scenario` owns parsed data, keys/events and YAML validation. Legacy
interfaces remain available. Calling a parsed spec's deprecated Execute/Check
method returns an explicit error; execution must pass through the runner.

`internal/testutil` holds shared network/HTTP fixtures. The old `sshd/xnet` and
`sshd/xhttp` paths forward to it for compatibility. `winpty` stays a vendored
package in the single module.

# Verification

Changes to these boundaries must keep SSH-only dependency checks, attachment
lifecycle tests, SSH behavior tests, and public Mosh API tests passing. Protocol
and PTY changes also require the Debian interoperability and actual tmux suite,
including resize, application cursor keys, terminal restoration and cancellation.
The CI interoperability job targets `./mosh ./internal/mosh/...`.

# Shared SSH client configuration

`client.Resolve` captures explicit config/environment authority and returns reusable
options. It owns bounded Host/Include parsing, identity and trust-path selection,
and language-only SendEnv selection. `client.Dial` resolves implicitly when needed;
session owners resolve first to apply `SendEnvironment` using `Session.Setenv`.
The authentication owner retains agent connections only through synchronous
handshake signatures, with cancellation and finite response/identity limits.
Encrypted keys with public headers are decrypted only when a signature is needed;
a matching loaded agent avoids decryption. No library operation inherits HOME,
SSH_AUTH_SOCK or config sources. Existing `Connect` remains the CLI-policy adapter.

Relative Include paths follow the user's Home/.ssh root (system ssh_config uses
/etc/ssh); only final-component wildcards are admitted. Match, proxy/jump, active
forwarding and positive keepalive policies refuse. GSSAPIAuthentication is a
boolean capability preference: this client offers public-key and explicit
password methods only, as a client built without GSSAPI does. Language SendEnv
requests are best-effort session requests, not authentication input.

The config lexer/host matcher and identity-order policy reuse the preserved Tigo
#59 source; other archived transport
and cryptographic code remains parked. The concrete source ref is
`refs/snapshots/tigo-serial-20261017/md59` at
`12b863ea22c429b93c42fccf2fadbff4528c3323`.

# Interaction and trust publication

`client.Interaction` supplies synchronous context-aware confirmation/passphrase
callbacks. Nil callbacks and BatchMode do not prompt. `TerminalInteraction` owns
the controlling terminal, raw-mode restoration, polling/cancellation and hidden
secret input on Unix; other systems return interaction-required until qualified.
No background terminal reader consumes caller input. The dial owner suspends
its network deadline during callbacks; caller context still closes the connection.

The default Dial trust policy asks only for unknown ordinary host keys, and
refuses changed/revoked keys and unknown certificate authorities. Trust is parsed
from bounded regular-file snapshots using upstream key/certificate primitives.
Accepted keys go to the selected user file under an exclusive .lock, with fresh
trust checks, byte/identity guards, permission preservation and atomic rename.
New files use 0600, directories 0700; symlinked write destinations refuse. Locks
coordinate cooperating writers; guards do not promise atomic exclusion of
arbitrary writers. `TrustWriteError.Published` reports late durability errors.
Trust persists independently of subsequent authentication/repository effects.
Legacy Connect keeps its prior private trust and replacement choices; its key,
agent, dialing and Unix terminal implementation now shares these client owners.
Legacy Connect retains its previous blocking terminal adapter on non-Unix systems;
that adapter is not exposed as the cancellable Dial interaction API.

Windows has no implicit `/etc/ssh/ssh_known_hosts` source; callers can supply
system trust paths explicitly. Trust publication syncs the file on every platform
and the parent directory on Unix; Unix permission modes do not configure Windows ACLs.

The bounded snapshot matcher is adapted from the same archived #59 trust source,
without copied cryptographic internals. This does not qualify the whole #59 task.
