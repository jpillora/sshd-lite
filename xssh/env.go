package xssh

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// envKeyMatches reports whether an environment entry ("NAME=value") names the
// given variable. Windows environment names are case-insensitive, so a client
// sending PATH must replace an inherited Path rather than sit beside it and
// leave which one wins to chance.
func envKeyMatches(entry, name string) bool {
	if len(entry) <= len(name) || entry[len(name)] != '=' {
		return false
	}
	if envNamesCaseInsensitive {
		return strings.EqualFold(entry[:len(name)], name)
	}
	return entry[:len(name)] == name
}

func appendEnv(env []string, kv string) []string {
	name, _, ok := strings.Cut(kv, "=")
	if !ok {
		return append(env, kv)
	}
	for i, e := range env {
		if envKeyMatches(e, name) {
			env[i] = kv
			return env
		}
	}
	return append(env, kv)
}

func hasEnv(env []string, key string) bool {
	for _, e := range env {
		if envKeyMatches(e, key) {
			return true
		}
	}
	return false
}

// baseEnv returns the environment a new session starts from.
//
// The server process environment is deliberately not inherited by default.
// sshd-lite runs shells and commands as the user that started it, so
// inheriting would hand every authenticated client whatever the operator
// happened to have exported — cloud credentials, API tokens, CI secrets — with
// no way to opt out. OpenSSH builds a session environment from scratch for the
// same reason. Config.InheritEnv restores the old behaviour for callers that
// depend on it.
func baseEnv(inherit bool) []string {
	if inherit {
		return os.Environ()
	}
	env := make([]string, 0, len(baseEnvNames))
	for _, name := range baseEnvNames {
		// Windows environment lookups are case-insensitive, so the canonical
		// spellings in baseEnvNames match however the process received them.
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// connectionEnv returns the SSH_CLIENT and SSH_CONNECTION variables OpenSSH
// exports, so tooling can identify the peer. Both are omitted when either
// address cannot be split, rather than exporting a half-formed value.
//
// SSH_TTY is not set. OpenSSH allocates the pty before forking, so it knows the
// slave name in time to put it in the child's environment; startPTY allocates
// and starts in one step because the Windows ConPTY backend cannot separate
// them, and by then the environment is already fixed.
func connectionEnv(remote, local net.Addr) []string {
	remoteHost, remotePort, ok := splitAddr(remote)
	if !ok {
		return nil
	}
	localHost, localPort, ok := splitAddr(local)
	if !ok {
		return nil
	}
	return []string{
		fmt.Sprintf("SSH_CLIENT=%s %s %s", remoteHost, remotePort, localPort),
		fmt.Sprintf("SSH_CONNECTION=%s %s %s %s", remoteHost, remotePort, localHost, localPort),
	}
}

func splitAddr(a net.Addr) (host, port string, ok bool) {
	if a == nil {
		return "", "", false
	}
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return "", "", false
	}
	return host, port, true
}
