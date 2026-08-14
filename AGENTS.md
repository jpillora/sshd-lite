# AGENTS.md

This document provides guidance for AI assistants working on sshd-lite.

## Project Overview

sshd-lite is a lightweight SSH daemon written in Go. It supports:

- Interactive shells (bash on Linux/macOS, PowerShell on Windows)
- Remote command execution with SSH stdin, stdout, stderr, and exit-status semantics
- Password and public key authentication
- SFTP subsystem (`--sftp`)
- Local and reverse TCP forwarding (`--tcp-forwarding`)
- Ed25519 and RSA server keys

Authenticated SSH names do not select operating-system users. Shells and commands run with the same privileges as the sshd-lite process; there is no system-user lookup or privilege switching.

## Development Commands

### Testing

```bash
go test ./...                 # Run all tests
go test -race ./...           # Run the test suite with the race detector
go test -v ./sshd ./xssh      # Run the core package tests verbosely
go vet ./...                  # Vet the module
go generate ./...             # Regenerate README CLI help from main.go
```

### Building

```bash
go build .             # Build the main binary
go run . --help        # Run with help output
```

### Running the Server

```bash
go run . user:pass                    # Basic auth on default port (22, fallback 2200)
go run . --port 2222 user:pass        # Custom port
go run . --keyseed test user:pass     # Seeded RSA key
go run . --keyseed test --keyseed-ec user:pass  # Ed25519 key
go run . --sftp user:pass             # Enable SFTP
go run . --tcp-forwarding user:pass   # Enable TCP forwarding
```

### CI/Release

- Every push and pull request builds and tests with stable Go on Ubuntu, macOS, and Windows.
- Tags matching `v*` trigger GoReleaser binary publication and multi-platform Docker publication to GHCR after tests pass.

## Key Files

| File | Purpose |
|------|---------|
| `main.go` | CLI entrypoint |
| `sshd/config.go` | Public high-level server configuration and handler types |
| `sshd/server.go` | Listener lifecycle, context shutdown, and handshake admission |
| `sshd/server_config.go` | Host-key, shell/workdir, and authentication setup |
| `sshd/server_conn.go` | SSH handshake and per-connection dispatch |
| `sshd/handler_validation.go` | High-level built-in/custom handler conflict checks |
| `sshd/key/` | RSA/Ed25519 key generation and authorized-key parsing |
| `xssh/config.go` | Lower-level SSH connection configuration and handler APIs |
| `xssh/conn.go` | Protocol handler registration and connection serving |
| `xssh/session_handler.go` | PTY, shell, environment, and exec request handling |
| `xssh/sftp.go` | SFTP subsystem implementation |
| `xssh/tcp_fwd.go` | Local and reverse TCP forwarding |
| `server/compat.go` | Deprecated compatibility aliases for older imports |
| `sshd/sshtest/` | Integration harness, scenarios, and protocol test support |
| `winpty/` | Vendored Windows ConPTY backend |

## Modules

The repository is a single Go module. There is no `go.work` and no nested
`go.mod`: `winpty` is an ordinary package, so `go build`, `go test` and
`go mod tidy` behave the same here as they do for any consumer.

- The root module uses `github.com/creack/pty` on Unix and the vendored
  `winpty` package on Windows.
- Do not add a `go.work` or a nested `go.mod` back. `winpty` was a separate
  module purely to host a `replace` directive; that replace was ignored by every
  consumer, which made the package impossible to import or `go install`, and the
  workspace hid the breakage from CI.

## Common Issues

- **Ed25519 keys**: Use `ssh.MarshalPrivateKey` to serialize Ed25519 keys, not raw bytes.
- **Work directory**: `sshd.NewServer` resolves an empty `Config.WorkDir` to the process working directory. Shells, exec commands, and high-level SFTP all use that directory.
- **Authorized keys**: File authentication accepts unrestricted keys only. Any parsed entry with options rejects the whole file. The file is reloaded for every public-key authentication, and reload errors deny authentication until it is valid again.
- **Auth argument parsing**: `Config.AuthType` is read as `user:pass` when it contains a colon, except for a drive-qualified path such as `C:\keys\authorized_keys`, which is always treated as a file path. Reading one as a credential pair would silently enable password authentication with the drive letter as the user.
- **Programmatic keys**: `Config.AuthKeys` is mutually exclusive with `AuthType`; it accepts bare public keys and cannot express `authorized_keys` options, username bindings, or per-key restrictions.
- **Handshake protection**: Zero `HandshakeTimeout` and `MaxPendingHandshakes` values select the defaults (10 seconds and 64). Negative values disable the corresponding protection.
- **Handler conflicts**: `sshd.NewServer` rejects custom handler names reserved by enabled built-ins. At the lower level, prefer `xssh.NewConnChecked` when configuration errors must be returned; `xssh.NewConn` panics on conflicts.
- **Windows PTY**: The `winpty` package vendors its ConPTY implementation (`*_windows.go`, from `photostorm/pty`) instead of depending on that fork. The fork's `go.mod` declares itself as `github.com/creack/pty`, so consuming it needs a `replace`, and a `replace` outside the main module is ignored. Do not reintroduce the replace. Vetting the Windows build reports four pre-existing `unsafe.Pointer` findings in the vendored files.
- **Port fallback**: With an empty port, the server tries 22 first and falls back to 2200.
- **Session environment**: Sessions do **not** inherit the server process's environment. Shells run as whoever started sshd-lite, so inheriting handed every authenticated client the operator's secrets. A session gets `baseEnvNames` (`xssh/env_unix.go`, `xssh/env_windows.go`) plus `SSH_CLIENT`/`SSH_CONNECTION`, the client's `env` requests, and `TERM` from its `pty-req`. `--inherit-env` restores the old behaviour. `--noenv` is unrelated: it drops *client*-supplied variables.
- **`SSH_TTY`**: Not set. OpenSSH allocates the pty before forking, so it knows the slave name in time; `startPTY` allocates and starts in one step because the Windows ConPTY backend cannot separate them.
- **Session teardown**: EOF on the client's stdin is not session cancellation, for interactive shells (`attachShell`) as well as exec (`copyCommandStdin`). Teardown is owned by the shell reaper, which also sends `exit-status`; closing the channel from the output-copy goroutine raced it and made every interactive session report 255.

# Meads (`md`) Task Tracking Context

## Overview

`md` is running in **git mode**: tasks live as git refs (`refs/meads/tasks/<id>`), not in a file. There is no `TASKS.md` or `TASKS.csv` in this repo — nothing to read or edit directly. Every command below is exactly the same as file mode; only the storage differs.

## Essential Commands

### Finding Work
- `md ready` - Show open tasks not blocked by dependencies (sorted by priority)
- `md list` - List all tasks
- `md list --json` - List all tasks as JSON
- `md list --tag=api` - List tasks carrying a tag (comma-separated requires all of them, e.g. `--tag=api,backend`)
- `md ready --tag=api` - Same filter over ready work
- `md list --history` - List all tasks from git history (including deleted)
- `md get <id>` - Get a specific task (a soft-deleted task's ref is kept forever, so this still resolves it)
- `md get --json <id>` - Get a specific task as JSON

### Creating Tasks
- `md add "Fix the login bug"` - Add a simple task
- `md add "bug: Fix login P1. Session cookie expires"` - Rich input parsing
  - Type prefix: `bug:`, `task:`, `feature:`, `idea:` (optional)
  - Priority: `P0`-`P9` (0=critical, 4=backlog, default=P2)
  - Title: text before the first `. ` (period+space) or newline
  - Description: text after that split point
- `md add --title="Fix login" --type=bug --priority=P1 --description="Details here"` - Flag-based
- `md add --title="Fix login" --description-file=/path/to/notes.md` - Description from file
- `md add --title="Fix login" --tags=api,web-ui` - Set tags (comma-separated; each tag is lowercase letters, numbers and dashes)

### Updating Tasks
- `md update <id> --status=draft|open|inprogress|closed` - Update status
- `md update <id> --priority=P1` - Update priority
- `md update <id> --title="New title"` - Update title
- `md update <id> --description-file=/path/to/notes.md` - Update description from file
- `md update <id> --tags=api,web-ui` - Replace all tags (`--tags=` clears them)
- `md update <id> --add-tags=docs` / `md update <id> --rm-tags=api` - Add or remove tags, keeping the rest
- `md set-status <id> <status>` - Shorthand for status changes
- `md del <id>` - Delete a task (soft delete — see Rules)

### Dependencies
- `md add-dep <child> <parent>` - Make child depend on parent
- `md rm-dep <child> <parent>` - Remove child's dependency
- `md add --depends-on=<id> "Task title"` - Add task with dependency
- Tasks blocked by unclosed dependencies are excluded from `md ready`

## Common Workflows

**Starting a session:**
```bash
md ready              # Find available work
md get <id>           # Review task details
md set-status <id> inprogress  # Claim it
```

**Creating dependent tasks:**
```bash
md add "feature: Build API endpoint"    # Returns ID, e.g. 5
md add "Write tests for API. Cover edge cases" --depends-on=5
```

**Completing work:**
```bash
md set-status <id> closed    # Mark task done
```

## Rules
- **There is no task file** - do NOT look for or try to edit `TASKS.md`/`TASKS.csv`; it does not exist in git mode. Always use `md` commands to read and modify tasks, never raw `git` plumbing on `refs/meads/*`.
- **Nothing to stage or commit yourself** - every `md add`/`update`/`set-status`/`add-dep`/`rm-dep`/`del` commits straight to that task's own ref the moment it runs. There is no "commit the tasks file" step.
- If a remote (`origin`) is configured, meads pushes `refs/meads/*` there automatically — you do not need to `git push` for task changes to reach it. The push runs at most once per `pushInterval` (default 1m), so roughly one command per interval waits for it; it is bounded by a timeout and never fails your command if the remote is unreachable.
- `md del` never removes anything - it soft-deletes (the ref is kept forever), so a deleted id is never reused and `md get <id>` still resolves it.
- Concurrent writes are safe via compare-and-swap on each task's own ref.
- `md auto-save` and `md auto-delete` are file-mode git hooks; both no-op in git mode (there is no tasks file to stage or prune).
- `md beads-import` is not supported in git mode (it only imports into a tasks file).
- `md doctor` also detects tasks that have diverged after independent edits in two clones, and repairs duplicate ids left by two clones creating a task offline at the same id; a genuine divergence needs manual resolution.
