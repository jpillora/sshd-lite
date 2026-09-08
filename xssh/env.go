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

// baseEnv returns inherited process variables for a new session. Opting out
// retains the platform-specific variables needed to run a shell.
func baseEnv(noInherit bool) []string {
	if !noInherit {
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

// sessionEnv layers inherited variables over system defaults without changing
// the daemon's own environment. A missing system file is normal.
func sessionEnv(noInherit, noGlobal bool, path string) ([]string, error) {
	var env []string
	var err error
	if !noGlobal {
		env, err = readEnvironmentFile(path)
	}
	if env == nil {
		// exec.Cmd treats nil as full inheritance, even when opted out.
		env = []string{}
	}
	for _, kv := range baseEnv(noInherit) {
		env = appendEnv(env, kv)
	}
	return env, err
}

// readEnvironmentFile reads /etc/environment-style assignments, not a shell
// script: values are literal, with optional surrounding quotes and comments.
// Blank lines and malformed assignments are ignored.
func readEnvironmentFile(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimSpace(line[len("export"):])
		}
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || !validEnvName(name) || strings.ContainsRune(value, 0) {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			end := strings.IndexByte(value[1:], value[0])
			if end < 0 {
				continue
			}
			end++
			tail := strings.TrimSpace(value[end+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				continue
			}
			value = value[1:end]
		} else {
			value, _, _ = strings.Cut(value, "#")
			value = strings.TrimSpace(value)
		}
		env = appendEnv(env, name+"="+value)
	}
	return env, nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
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
