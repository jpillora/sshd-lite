package client

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sources is explicit local authority. Empty fields do not inherit process
// state. ConfigFiles are read in order; absent files are optional. Relative
// paths resolve from Directory. Home supplies ~/ and default identity paths.
type Sources struct {
	Home, Directory, LocalUser, AgentSocket string
	ConfigFiles, KnownHostsFiles            []string
	Environment                             map[string]string
}

// EnvironmentEntry is a selected, copied SendEnv value for a caller's session.
type EnvironmentEntry struct{ Name, Value string }

// Resolve captures configuration for a connection. Dial also calls Resolve.
// Callers that use SendEnv should resolve once, then pass the returned options
// to Dial and apply SendEnvironment to their session (server rejection is normal).
// Returned configuration (Host, User, Port, Sources, IdentityFiles, Timeout and
// SendEnvironment) is frozen: edits cause ErrResolvedOptionsChanged. Keep the
// original input to resolve a different target or policy. Credentials, Auth,
// HostKeyCallback and Interaction may be supplied per attempt. Nothing may be
// mutated during Resolve or Dial. Sources are copied and configuration is parsed
// once; identity and trust file contents are re-read for each Dial.
func Resolve(ctx context.Context, o DialOptions) (DialOptions, error) {
	if err := ctx.Err(); err != nil {
		return o, err
	}
	if o.resolved != nil {
		if !o.resolved.matches(o) {
			return o, ErrResolvedOptionsChanged
		}
		return o, nil
	}
	if o.Host == "" || len(o.Host) > 4096 || len(o.User) > 4096 || strings.ContainsAny(o.Host+o.User, "\x00\r\n") {
		return o, errors.New("ssh client: invalid target")
	}
	r := configReader{ctx: ctx, o: o, scalars: map[string][]string{}, work: 1 << 20, active: true}
	if len(o.Sources.ConfigFiles) > 64 || len(o.IdentityFiles) > 64 {
		return o, errors.New("ssh client: source count limit")
	}
	for _, name := range o.Sources.ConfigFiles {
		p, err := r.expand(name, false)
		if err != nil {
			return o, err
		}
		r.active = true
		base := filepath.Join(o.Sources.Home, ".ssh")
		if filepath.Clean(p) == "/etc/ssh/ssh_config" {
			base = "/etc/ssh"
		}
		if err := r.read(p, base, 0); err != nil {
			return o, err
		}
	}
	if err := r.admit(); err != nil {
		return o, err
	}
	scalar := func(k, fallback string) string {
		if v := r.scalars[k]; len(v) > 0 {
			return v[0]
		}
		return fallback
	}
	if o.User == "" {
		o.User = scalar("user", o.Sources.LocalUser)
	}
	if o.Port == "" {
		o.Port = scalar("port", "22")
	}
	// Expansion sees the original host for HostName, then the resolved host for paths.
	r.o.User = o.User
	r.o.Port = o.Port
	var err error
	o.Host, err = r.expand(scalar("hostname", o.Host), true)
	if err != nil {
		return o, err
	}
	r.o.Host = o.Host
	state := &resolvedConfig{only: scalar("identitiesonly", "no") == "yes", batch: scalar("batchmode", "no") == "yes", hash: scalar("hashknownhosts", "no") == "yes", strict: scalar("stricthostkeychecking", "ask"), pubkey: scalar("pubkeyauthentication", "yes") == "yes", password: scalar("passwordauthentication", "yes") == "yes"}
	state.alias, err = r.expand(scalar("hostkeyalias", o.Host), true)
	if err != nil {
		return o, err
	}
	if o.Timeout == 0 {
		if v := scalar("connecttimeout", ""); v != "" {
			n, _ := strconv.Atoi(v)
			if n > 0 {
				o.Timeout = time.Duration(n) * time.Second
			}
		}
	}
	if v, ok := r.scalars["identityagent"]; ok {
		o.Sources.AgentSocket = v[0]
		if v[0] == "SSH_AUTH_SOCK" {
			o.Sources.AgentSocket = r.o.Sources.AgentSocket
		}
	}
	if o.Sources.AgentSocket == "none" {
		o.Sources.AgentSocket = ""
	} else if o.Sources.AgentSocket != "" {
		o.Sources.AgentSocket, err = r.expand(o.Sources.AgentSocket, false)
		if err != nil {
			return o, err
		}
	}
	paths := append([]string(nil), o.IdentityFiles...)
	paths = append(paths, r.identities...)
	if len(paths) == 0 && o.Sources.Home != "" {
		paths = []string{"~/.ssh/id_rsa", "~/.ssh/id_ecdsa", "~/.ssh/id_ed25519"}
		state.defaults = true
	}
	if len(paths) > 64 {
		return o, errors.New("ssh client: identity count limit")
	}
	o.IdentityFiles = nil
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "none" || seen[p] {
			continue
		}
		seen[p] = true
		name, e := r.expand(p, false)
		if e != nil {
			return o, e
		}
		o.IdentityFiles = append(o.IdentityFiles, name)
	}
	known := append([]string(nil), o.Sources.KnownHostsFiles...)
	if known == nil {
		user := r.scalars["userknownhostsfile"]
		if user == nil && o.Sources.Home != "" {
			user = []string{"~/.ssh/known_hosts"}
		}
		known = append(known, user...)
		global := r.scalars["globalknownhostsfile"]
		// Windows has no implicit Unix system trust path. A rooted path such
		// as /etc/ssh/ssh_known_hosts is drive-relative there, not absolute.
		if global == nil && runtime.GOOS != "windows" {
			global = []string{"/etc/ssh/ssh_known_hosts"}
		}
		known = append(known, global...)
		if len(user) > 0 && user[0] != "none" {
			state.writeTrust, err = r.expand(user[0], false)
		}
	} else if len(known) > 0 && known[0] != "none" {
		state.writeTrust, err = r.expand(known[0], false)
	}
	if err != nil {
		return o, err
	}
	if len(known) > 64 {
		return o, errors.New("ssh client: trust source count limit")
	}
	for _, p := range known {
		if p == "none" {
			continue
		}
		p, e := r.expand(p, false)
		if e != nil {
			return o, e
		}
		state.known = append(state.known, p)
	}
	o.SendEnvironment = nil
	if len(o.Sources.Environment) > 256 {
		return o, errors.New("ssh client: environment count limit")
	}
	names := make([]string, 0, len(o.Sources.Environment))
	for n := range o.Sources.Environment {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		selected := false
		for _, p := range r.sendEnv {
			remove := strings.HasPrefix(p, "-")
			yes, e := sshGlob(strings.TrimPrefix(p, "-"), n, &r.work)
			if e != nil {
				return o, e
			}
			if yes {
				selected = !remove
			}
		}
		if selected {
			v := o.Sources.Environment[n]
			if len(n) > 256 || len(v) > 4096 || strings.ContainsAny(n+v, "\x00\r\n") {
				return o, errors.New("ssh client: invalid session environment")
			}
			o.SendEnvironment = append(o.SendEnvironment, EnvironmentEntry{n, v})
		}
	}
	o.resolved = state
	sealConfiguration(&o)
	return o, nil
}
