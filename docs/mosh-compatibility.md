# sshd-lite: Debian Mosh interoperability

Date: 9 September 2026

The sshd-lite client and embedded server now interoperate independently with Debian Mosh 1.4.0. The implementation was developed in `/mnt/data/projects/sshd-lite-mosh`, on branch `feat/mosh-client-server`, for integration into the primary branch, `master`.

The initial SSH/Mosh implementation was committed first as requested: `173f3f4` (`feat(mosh): add SSH client and shared-port UDP terminal sessions`). The completed compatibility, API and cohesion changes are recorded together in a follow-up commit for `master`. No remote push is part of this integration.

## Results

| Client | SSH bootstrap / Mosh server | Result |
|---|---|---|
| Debian `mosh` / `mosh-client` | sshd-lite embedded server | Passed: authentication, actual command execution, screen output, resize, normal shutdown |
| sshd-lite client | OpenSSH / Debian `mosh-server` | Passed: standard bootstrap, command execution, screen output, normal shutdown |
| Built sshd-lite client executable | OpenSSH / Debian `mosh-server` | Passed: CLI arguments, authentication, screen output, resize, shutdown |
| Debian launcher and client | Built sshd-lite server executable | Passed: `--mosh`, shared UDP port, actual command execution, screen output, shutdown |
| Debian client through UDP fault proxy | sshd-lite embedded server | Passed: packet loss, reordering, source-port change, exactly-once input effects, screen convergence, shutdown |
| sshd-lite client through UDP fault proxy | Debian server | Passed: the same fault scenarios |
| Debian client running tmux | sshd-lite embedded server | Passed: full-screen application, command execution inside tmux, detach, Ctrl-^ then `.` disconnect |

The host tests used the actual Debian ARM64 package `mosh_1.4.0-1+b1_arm64.deb`, extracted separately from Ubuntu's installed Mosh. The interoperability suite also passed in a Debian Bookworm container with Debian's packaged Mosh and OpenSSH. This establishes the tested combinations; it does not claim interoperability with every Mosh version or every terminal feature.

## Client changes

The client now opens an authenticated SSH session channel, requests a PTY, and executes the standard bootstrap:

```sh
MOSH_SERVER_NETWORK_TMOUT=300 'mosh-server' new -s -c 256 -l LANG=C.UTF-8
```

It parses `MOSH CONNECT <port> <key>`, accepting the standard 22-character unpadded base64 key. It then closes SSH and connects over UDP to the actual SSH peer at the advertised port. Bootstrap output is bounded to 64 KiB and startup to ten seconds.

`--mosh-server` selects another remote executable. Optional remote command arguments are individually shell-quoted and passed after `--`; they are literal arguments. The SSH authentication implementation now combines explicit identity and agent keys into one public-key method so an available agent cannot hide the specified identity.

Screen reconstruction starts at the PTY size negotiated during bootstrap. Each update is reconstructed from its named base state, then rendered relative to the currently displayed screen. This matters when a server sends cumulative updates or packets arrive out of order.

## Server changes: virtual executable in the SSH session

With `--mosh`, sshd-lite handles supported literal `mosh-server new ...` exec requests directly inside the authenticated session channel. It returns the standard connection line and successful SSH exit status. There is no generated executable file or external server process; the UDP session survives closure of the bootstrap channel.

An AST parser recognizes literal invocations and the standard launcher's exact remote-address probe. Supported options include `-s`, `-c`, locale-only `-l`, `-p`, `-i`, and `-- COMMAND...`. Ordinary commands, absolute executable paths, and unsupported shell structures retain normal SSH exec behavior. The lower-level session API has an optional `ExecHandler` hook for this purpose.

All embedded sessions share the SSH listener's numeric UDP port. A requested UDP port or range must include that port. An explicit bind address must match the SSH connection's local address. A fresh authenticated UDP packet starts the terminal; an issued but unused key does not start a shell.

The private JSON key request has been removed. Both the bundled client and Debian clients use standard session bootstrap.

## UDP protocol and lifetime

The implementation retains mosh-go's OCB encryption, fragmentation and protobuf codecs, with an adapted local State Synchronization Protocol transport. The adaptation includes:

- Applying incoming updates to their `old_num` base, including empty states.
- Tracking absolute user-action offsets so overlapping input updates do not execute keys twice.
- Advancing `throwaway_num` and pruning only states the sender has released.
- Separate fragment instruction IDs, stable for identical retransmissions.
- A bounded replay window that accepts unseen reordered fragments but rejects repeated nonces.
- Standard `UINT64_MAX` shutdown states and acknowledgements; the server briefly retains shutdown acknowledgement state after releasing the terminal.
- Delayed echo acknowledgements and explicit size updates.
- Correct timestamp replies that exclude time spent waiting locally and omit stale timestamps.

The server authenticates datagrams against a maximum of 64 session keys before routing them. An authenticated source-port/address change can move the session; an older reordered packet cannot move it back. Pending keys and active sessions expire five minutes after the last fresh authenticated inbound datagram. Encrypted keepalives maintain idle connected sessions. Invalid traffic, replayed packets, and outbound terminal output cannot renew a key.

Tests accelerate the timeout to verify issuance expiry, keepalive renewal, replay rejection, and shell cleanup; they do not wait five real minutes for every case. Decompression, terminal dimensions, retained state counts, and retained screen cells are bounded.

## Defects found during verification

The tests caught and drove fixes for incorrect shell backslash decoding, combining-character loss in the original emulator, incorrect initial screen dimensions, and timestamp echo behavior that overstated latency. The renderer now uses a pinned version of Charm's terminal emulator with the relevant grapheme fix.

The dimension bug was particularly significant: reconstructing a 100×30 session in an 80×24 emulator could garble scrolling and panic. The client now supplies its negotiated initial dimensions to the UDP session.

Additional race-checked tests exercise literal command arguments containing spaces, quotes, dollar signs, and backticks against both servers, and the built server executable against Debian’s launcher.

The source-port/loss test writes numbered records to a file and checks their exact contents, then checks a transformed screen marker after a large output update. Echoing an unexecuted shell command cannot satisfy those assertions. Debian fatal assertions and missing dependencies fail the required integration job instead of being silently accepted.

## Verification

Passed on the worktree:

```sh
go build -o /tmp/sshd-lite-mosh-interop .
MOSH_TEST_REQUIRED=1 \
MOSH_TEST_BIN=/tmp/sshd-lite-debian-mosh/usr/bin \
MOSH_TEST_LITE=/tmp/sshd-lite-mosh-interop go test ./...
MOSH_TEST_REQUIRED=1 \
MOSH_TEST_BIN=/tmp/sshd-lite-debian-mosh/usr/bin \
MOSH_TEST_LITE=/tmp/sshd-lite-mosh-interop go test -race ./...
go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=darwin GOARCH=arm64 go build ./...
```

Protocol regressions cover reversed fragmented delivery, replay rejection, 1,500 state transitions, input overlap, Unicode/color/cursor/mode reconstruction, bootstrap quoting and validation, and shutdown acknowledgement loss. Six consecutive repetitions of both mixed fault-proxy pairings passed after the timestamp correction.

`go mod tidy`, formatting, README generation, and diff whitespace checks were completed. CI now includes a Debian Bookworm interoperability job installing Mosh, OpenSSH, sshpass, and tmux. It builds the CLI and requires all integration dependencies; release jobs depend on this job as well as the existing platform tests. The workflow is added locally and has not been run on GitHub because nothing was pushed.

## Usage

```sh
# Embedded server: TCP and UDP 2222
sshd-lite --mosh --port 2222 user:pass

# Debian client to embedded server
mosh --ssh='ssh -p 2222' user@host

# sshd-lite client to embedded server
sshd-lite client --mosh --port 2222 user@host

# sshd-lite client to Debian server behind OpenSSH
sshd-lite client --mosh user@debian-host

# A literal terminal command
sshd-lite client --mosh --port 2222 user@host tmux new-session
```

Open both TCP and UDP on the selected server port. Normal host-key verification applies to the SSH bootstrap.

## Remaining limits

- Mosh synchronizes the current screen, not scrollback. A surviving client can resume its session after a temporary outage within the idle grace period; there is no saved-session reattachment feature after restarting the client.
- Standard Mosh does not report a remote process exit status. Mixed sessions return success on clean protocol shutdown. Two sshd-lite peers can negotiate an optional exit-status extension.
- The simple client has no predictive local echo. Sending is deliberately conservative, with one input/output state in flight; it is not a performance-equivalent replacement for all of Debian Mosh's scheduling and prediction behavior.
- Terminal behavior depends on the emulator's supported escape sequences. The verified Unicode, colors, cursor/modes, resizing, and tmux scenarios are not exhaustive terminal conformance. Graphemes split across separate emulator writes remain an area for further testing.
- Windows builds, but running ConPTY resize remains unsupported by the existing backend. Windows runtime interoperability was not tested.
- For multihomed servers, bind sshd-lite to the intended local address. The shared UDP socket does not implement per-session source-address selection, arbitrary UDP allocation, or all custom launcher shell wrappers.
- Use the virtual bootstrap for an embedded server. Executing a real forking `mosh-server` through sshd-lite's ordinary exec path can conflict with its existing process-group cleanup. The reference-server pairing deliberately uses OpenSSH; existing SSH cleanup semantics were preserved.

## Follow-up: actual terminals and adversarial review

The follow-up uses tmux 3.4 as the receiving terminal, independently of the Go emulator used by the implementation. A separate remote tmux runs behind each Mosh connection. `TestMoshInActualTmuxTerminals` exercises Debian client → embedded server, built Lite client → Debian server, and Lite client → embedded server.

All three pairings passed actual TTY checks, readline arrow editing, colored CJK and combining-character output, pane splitting/switching, PTY resizes to 73×19, 120×42 and 100×30, tmux history/copy mode, detach/reattach, normal exit and Ctrl-^ then `.`. The tests compare terminal settings before and after each session and check that the local shell remains usable. sshd-lite does not clear or switch screens on connect; a remote full-screen application such as tmux can select and restore its own alternate screen, as it does over ordinary SSH. These are automated Linux PTY/tmux tests, not a claim of testing every graphical terminal application.

This uncovered two client defects: the first display update could leave stale
cells visible, and an intentional escape disconnect returned an error. The
client renders the first received state in full, restores terminal modes without
eagerly clearing or switching screens on connect, and treats its explicit escape
as a successful disconnect.

A requested subagent review then confirmed two further defects:

| Finding | Fix and verification |
|---|---|
| Application/normal cursor-key encoding differed from standard Mosh. Readline's acceptance of both encodings had hidden the problem. | The Lite CLI enables application cursor mode during Mosh; the server translates unmodified SS3 arrows when the remote process requests normal mode. A raw-terminal helper checks exact Up-arrow bytes in both modes across all three pairings, and a unit test splits sequences across packets. The reviewer independently retested the fix. |
| A process generating many terminal queries without reading replies could block server shutdown inside the emulator's synchronous response pipe. | Response handling is bounded and cancels an overloaded session; cancellation independently closes the response pipe. A regression with blocked terminal writes verifies that query backpressure releases the session and server shutdown. This reproduction uses a controlled terminal implementation rather than an actual blocked PTY process. |

The full regression run also exposed concurrent bootstrap parsing sharing mutable buffers in the shell-expansion library's default configuration. Each parse now owns its expansion configuration; a parallel parsing regression passes under the race detector.

The changes are covered by the full repository suite, the affected packages' race tests (including a race-built CLI), and `go vet`. Test fixtures wait for tmux's actual PTY dimensions and completed copy-mode transitions, which are asynchronous relative to layout commands.

## Follow-up: public Go API

Both server and client can run in a Go application without launching an SSH,
Mosh, or sshd-lite executable. The runnable `example/mosh/main.go` creates
in-memory keys, starts an authenticated embedded server on an ephemeral loopback
port, verifies its host key, exchanges terminal input/output, receives exit
status 7, and shuts down through contexts.

- Server: `sshd.Config{Attach: mosh.Attach}`, `sshd.NewServer`, and `StartContext` or
  `StartWithContext` enable the same shared TCP/UDP port and session lifecycle.
- Client: `mosh.Dial` accepts explicit `*ssh.ClientConfig` authentication
  and host verification. `mosh.Start` consumes a dedicated existing SSH
  connection, closing it before returning on success or failure.
- `mosh.ClientConfig` sets terminal type, size, remote executable/argv and an
  `io.Writer` for ANSI output. `mosh.Session` supports concurrent `Write`,
  `Resize`, `Done`, repeatable `Wait`, and idempotent `Close`.
- The CLI delegates session handling to this API and retains its own terminal
  modes, stdin, resize signals and escape handling. The library does none of
  those local-terminal operations and forwards literal escape input.

Public-package integration tests cover both constructors, SSH connection
ownership, copied input, actual PTY resizing, native exit status, cancellation,
concurrent writes/resizes and shutdown, host verification/authentication
failures, and output errors including short writes. A stalled SSH channel-open
regression verifies cancellation before bootstrap can create its session.
A separate public API test connects to OpenSSH plus Debian Mosh and verifies
standard-peer output and shutdown behavior.

The output writer belongs to the application and must return promptly;
applications must unblock an arbitrary blocking writer before awaiting shutdown.
Output is ANSI screen state, and Mosh has no stdin EOF operation. Pending resizes
are asynchronous. Unmodified cursor keys use standard Mosh application-mode
SS3 encoding. These contracts and an embedding example are documented in the
[Mosh guide](mosh-guide.md).

After the API refactor, the full repository suite passed with Debian integration
dependencies required, including all three actual tmux pairings. The client and
protocol packages passed under the race detector with a separately race-built
CLI. `go vet ./...`, Windows amd64 and macOS arm64 cross-builds, README generation,
`git diff --check`, and `go run ./example/mosh` also passed. The main checkout
remains clean.

The public API changes are included in the completed implementation.

## Follow-up: cohesion and optional Mosh dependencies

Mosh is now explicitly imported from `github.com/jpillora/sshd-lite/mosh`.
The CLI retains its `--mosh` flags. Embedded servers opt in with
`Attach: mosh.Attach`; clients use `mosh.Dial`, `mosh.Start`,
`mosh.ClientConfig`, and `mosh.Session`. These replace the unreleased branch's
`sshd.Config.Mosh` and `client.*Mosh` APIs. Existing SSH APIs remain available.

SSH-only imports of `sshd`, `client`, and `xssh` exclude Mosh, its emulator,
and its shell parser. An architecture regression checks this with `go list`.
The root CLI composes the optional packages. The repository still has one module,
and its default executable includes both protocols.

The internal Mosh session layer now depends on separate SSP and display packages.
A terminal-I/O owner manages its pumps, response pipe, and cancellation. Shared
PTY/process ownership lives in `internal/terminal`; SSH and Mosh adapters retain
their distinct shutdown policies. The listener attachment has regressions for failed startup cleanup, handler
isolation, and independent concurrent shutdown.

The test harness is organized by client resource and lifecycle responsibility.
Execution implementations receive typed environments; compatibility adapters
retain the existing exported interfaces. Parsed scenario specs now return an
error if called as executable actions instead of silently reporting success.
Network test helpers share an internal package, with legacy public wrappers.

See `docs/architecture.md` for the final layout and dependency rules.

The completed refactor passed fresh full-repository tests and race tests, with
Debian dependencies required and separate normal/race-built CLI binaries.
All three actual tmux pairings passed again, along with the public Mosh API,
SSH-only dependency check, attachment failure/isolation tests, and existing SSH,
SFTP and forwarding regressions. `go vet ./...`, Windows amd64 and macOS arm64
cross-builds, README generation, whitespace checks, and the in-process example
also passed. Windows runtime behavior remains subject to the limitation above.

```sh
go build -o /tmp/sshd-lite-mosh-interop .
go build -race -o /tmp/sshd-lite-mosh-race .
MOSH_TEST_REQUIRED=1 MOSH_TEST_BIN=/tmp/sshd-lite-debian-mosh/usr/bin \
  MOSH_TEST_LITE=/tmp/sshd-lite-mosh-interop go test -count=1 ./...
MOSH_TEST_REQUIRED=1 MOSH_TEST_BIN=/tmp/sshd-lite-debian-mosh/usr/bin \
  MOSH_TEST_LITE=/tmp/sshd-lite-mosh-race go test -race -count=1 ./...
```

The refactor is included in the completed implementation.

## Follow-up: smaller SSH/Mosh seam

The general service interface has been replaced with `sshd.Config.Attach`, a
single per-listener callback returning an `xssh.ExecHandler` and an `io.Closer`.
Enable it with `Attach: mosh.Attach`. There are no service names, registry,
validation interface, or handler-map mutations. The callback receives only a
context and listener address; the SSH server owns its returned closer.

Mosh uses `xssh` directly and no longer imports either high-level SSH package.
The adapter supports only standard session bootstrap; the old `mosh@sshd-lite`
JSON request is rejected even when Mosh is enabled. It snapshots terminal launch
settings so a pending UDP session does not retain its bootstrap SSH session.
Shared-port binding, failure cleanup, ordinary SSH exec fallback, concurrent
listener isolation, and independent session lifetime remain covered by tests.

After reducing the seam, fresh full-repository tests and race tests passed with
Debian dependencies required and separately built normal/race CLI executables.
All three actual tmux pairings passed again. Vet, Windows/macOS cross-builds,
README generation, diff checks, and the in-process example also passed.
The reduced attachment API is included in the completed implementation.

## Sources

- [mosh-go v0.5.2](https://github.com/unixshells/mosh-go/tree/v0.5.2): encryption, codecs, and transport starting point; attribution retained in `internal/mosh/LICENSE.mosh-go`.
- [Mosh 1.4.0 source](https://github.com/mobile-shell/mosh/tree/mosh-1.4.0): reference bootstrap, state synchronization, terminal updates, and shutdown behavior.
- [Debian ARM64 package used on the host](https://deb.debian.org/debian/pool/main/m/mosh/mosh_1.4.0-1+b1_arm64.deb).
- [Charm terminal emulator](https://github.com/charmbracelet/x/tree/3986e9119cf9/vt): pinned as `v0.0.0-20260906004030-3986e9119cf9`.
