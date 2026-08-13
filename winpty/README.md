# winpty

Windows ConPTY support for sshd-lite, with a small `creack/pty`-shaped API
(`Start`, `StartWithSize`, `Setsize`). Off Windows every entry point returns
`ErrUnsupported` — only `xssh/pty_win.go` imports this package.

The ConPTY implementation is vendored from
[photostorm/pty](https://github.com/photostorm/pty), a fork of
[creack/pty](https://github.com/creack/pty) that added Windows support. See
`LICENSE-creack-pty` for the upstream copyright.

## Why the code is vendored rather than imported

The fork's `go.mod` still declares its module path as `github.com/creack/pty`,
so it can only be depended on through a `replace` directive. Go ignores
`replace` directives in any module that is not the main module, so that replace
only ever applied inside this repo's workspace: every external importer, and
`go install`, resolved the real `creack/pty` — which has no `Pty` or `FdHolder`
type — and failed to compile.

Vendoring removes the replace, which makes this module importable and
installable, and stops the replace from leaking up and silently swapping the
root module's `creack/pty` on Linux and macOS too.

It also fixes the version pin in place. Later photostorm releases deleted the
ConPTY implementation, so the pinned pseudo-version could never be bumped; there
is now no version to pin.
