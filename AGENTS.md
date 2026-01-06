# AGENTS.md

This document provides guidance for AI assistants working on sshd-lite.

## Project Overview

sshd-lite is a lightweight SSH daemon written in Go. It supports:
- Remote shells (bash on Linux/Mac, powershell on Windows)
- Password and public key authentication
- SFTP subsystem (`--sftp`)
- TCP forwarding (`--tcp-forwarding`)
- Ed25519 and RSA server keys

## Development Commands

### Testing
```bash
go test ./...          # Run all tests
go test -v ./server    # Run server tests with verbose output
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
- Push to `master` triggers tests on Windows/macOS/Ubuntu
- Tags matching `v*` trigger GoReleaser to build and release binaries
- Docker images are published to GHCR

## Key Files

| File | Purpose |
|------|---------|
| `main.go` | CLI entrypoint |
| `server/server.go` | SSH server main loop |
| `server/server_config.go` | SSH config and auth setup |
| `server/key_utils.go` | Key generation (RSA/Ed25519) |
| `server/pty_unix.go` | PTY handling for Unix |
| `server/pty_win.go` | PTY handling for Windows |
| `go.work` | Go workspace (main + winpty modules) |

## Go Workspace

The project uses Go workspaces to manage the `winpty` subdirectory as a separate module:
- `go.work` declares both `.` and `./winpty` as workspace members
- No `replace` directives needed in go.mod
- Run `go mod tidy` to update dependencies

## Common Issues

- **Ed25519 keys**: Use `ssh.MarshalPrivateKey` to serialize Ed25519 keys, not raw bytes
- **Windows PTY**: The winpty module uses a replace directive for `github.com/creack/pty`
- **Port fallback**: Server tries 22 first, falls back to 2200 if in use
