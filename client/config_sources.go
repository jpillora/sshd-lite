package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// UnsupportedError names an SSH policy that cannot be honored, without
// including its potentially secret value.
type UnsupportedError struct{ Directive string }

func (e *UnsupportedError) Error() string {
	return "ssh client: unsupported SSH directive: " + e.Directive
}

type resolvedConfig struct {
	only, batch, hash         bool
	strict, alias, writeTrust string
	known                     []string
	pubkey, password          bool
	defaults                  bool
}

type configReader struct {
	ctx                                context.Context
	o                                  DialOptions
	scalars                            map[string][]string
	identities, sendEnv                []string
	bytes, files, lines, work, entries int
	active                             bool
}

// Resolve captures configuration for a connection. Dial also calls Resolve.
// Callers that use SendEnv should resolve once, then pass the returned options
// to Dial and apply SendEnvironment to their session (server rejection is normal).
// Returned options may be reused, but must not be mutated during use.
func Resolve(ctx context.Context, o DialOptions) (DialOptions, error) {
	if err := ctx.Err(); err != nil {
		return o, err
	}
	if o.resolved != nil {
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
		if global == nil {
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
	return o, nil
}

func (r *configReader) expand(p string, value bool) (string, error) {
	if len(p) > 4096 || strings.ContainsAny(p, "\x00\r\n") {
		return "", errors.New("ssh client: invalid configuration value")
	}
	if strings.HasPrefix(p, "~/") {
		if r.o.Sources.Home == "" {
			return "", errors.New("ssh client: home directory is required")
		}
		p = filepath.Join(r.o.Sources.Home, p[2:])
	} else if strings.HasPrefix(p, "~") {
		return "", &UnsupportedError{"~user expansion"}
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '%' {
			b.WriteByte(p[i])
			continue
		}
		i++
		if i == len(p) {
			return "", errors.New("ssh client: incomplete percent expansion")
		}
		switch p[i] {
		case '%':
			b.WriteByte('%')
		case 'd':
			b.WriteString(r.o.Sources.Home)
		case 'h':
			b.WriteString(r.o.Host)
		case 'r':
			b.WriteString(r.o.User)
		case 'p':
			b.WriteString(r.o.Port)
		case 'u':
			b.WriteString(r.o.Sources.LocalUser)
		default:
			return "", &UnsupportedError{"percent expansion"}
		}
		if b.Len() > 4096 {
			return "", errors.New("ssh client: expanded path limit")
		}
	}
	p = b.String()
	if strings.Contains(p, "${") {
		return "", &UnsupportedError{"environment expansion"}
	}
	if !value && !filepath.IsAbs(p) {
		if !filepath.IsAbs(r.o.Sources.Directory) {
			return "", errors.New("ssh client: absolute source directory is required")
		}
		p = filepath.Join(r.o.Sources.Directory, p)
	}
	return p, nil
}

func readSource(ctx context.Context, name string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openSource(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("ssh client: nonregular or oversized source")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("ssh client: source byte limit")
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || int64(len(b)) != info.Size() {
		return nil, errors.New("ssh client: source changed while reading")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func (r *configReader) read(name, base string, depth int) error {
	if depth > 16 || r.files >= 256 {
		return errors.New("ssh client: configuration include limit")
	}
	r.files++
	b, err := readSource(r.ctx, name, 1<<20)
	if err != nil {
		return fmt.Errorf("ssh client: read config: %w", err)
	}
	r.bytes += len(b)
	if r.bytes > 4<<20 {
		return errors.New("ssh client: configuration byte limit")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.lines++
		if r.lines > 65536 || len(line) > 64<<10 {
			return errors.New("ssh client: configuration line limit")
		}
		w, err := sshWords(line)
		if err != nil {
			return err
		}
		if len(w) == 0 {
			continue
		}
		if len(w) < 2 {
			return errors.New("ssh client: missing directive value")
		}
		k := strings.ToLower(w[0])
		v := w[1:]
		if k == "host" {
			r.active, err = sshPatternList(v, r.o.Host, &r.work)
			if err != nil {
				return err
			}
			continue
		}
		if k == "match" {
			return &UnsupportedError{"Match"}
		}
		if !r.active {
			continue
		}
		if k == "include" {
			for _, p := range v {
				if err := r.include(p, base, depth+1); err != nil {
					return err
				}
			}
			continue
		}
		if k == "identityfile" {
			if len(v) != 1 {
				return errors.New("ssh client: invalid IdentityFile")
			}
			if len(r.identities) >= 64 {
				return errors.New("ssh client: identity count limit")
			}
			r.identities = append(r.identities, v[0])
			continue
		}
		if k == "sendenv" {
			if len(r.sendEnv)+len(v) > 128 {
				return errors.New("ssh client: SendEnv limit")
			}
			r.sendEnv = append(r.sendEnv, v...)
			continue
		}
		if _, ok := r.scalars[k]; !ok {
			if len(r.scalars) >= 256 {
				return errors.New("ssh client: directive count limit")
			}
			r.scalars[k] = v
		}
	}
	return nil
}

func (r *configReader) include(pattern, base string, depth int) error {
	if !filepath.IsAbs(pattern) && !strings.HasPrefix(pattern, "~") {
		pattern = filepath.Join(base, pattern)
	}
	p, err := r.expand(pattern, false)
	if err != nil {
		return err
	}
	if strings.ContainsAny(filepath.Dir(p), "*?[") {
		return &UnsupportedError{"Include directory wildcard"}
	}
	if !strings.ContainsAny(filepath.Base(p), "*?[") {
		return r.read(p, base, depth)
	}
	d, err := os.Open(filepath.Dir(p))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer d.Close()
	var matches []string
	for {
		batch, e := d.ReadDir(64)
		for _, entry := range batch {
			r.entries++
			if r.entries > 4096 {
				return errors.New("ssh client: Include directory entry limit")
			}
			yes, err := filepath.Match(filepath.Base(p), entry.Name())
			if err != nil {
				return err
			}
			if yes && !entry.IsDir() {
				matches = append(matches, entry.Name())
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	sort.Strings(matches)
	for _, name := range matches {
		if err := r.read(filepath.Join(filepath.Dir(p), name), base, depth); err != nil {
			return err
		}
	}
	return nil
}

func (r *configReader) admit() error {
	scalar := map[string]bool{"hostname": true, "user": true, "port": true, "identityagent": true, "hostkeyalias": true, "connecttimeout": true, "stricthostkeychecking": true}
	bools := map[string]bool{"identitiesonly": true, "batchmode": true, "hashknownhosts": true, "pubkeyauthentication": true, "passwordauthentication": true, "gssapiauthentication": true}
	inert := map[string]string{"proxycommand": "none", "proxyjump": "none", "forwardagent": "no", "forwardx11": "no", "forwardx11trusted": "no", "requesttty": "no", "canonicalizehostname": "no", "updatehostkeys": "no", "checkhostip": "no", "verifyhostkeydns": "no", "controlmaster": "no", "controlpath": "none", "serveraliveinterval": "0", "addkeystoagent": "no", "permitlocalcommand": "no", "kbdinteractiveauthentication": "no", "challengeresponseauthentication": "no", "gssapidelegatecredentials": "no", "gssapikeyexchange": "no", "connectionattempts": "1", "addressfamily": "any"}
	for k, v := range r.scalars {
		if k == "userknownhostsfile" || k == "globalknownhostsfile" {
			continue
		}
		if (k == "proxycommand" || k == "proxyjump") && (len(v) != 1 || v[0] != "none") {
			return &UnsupportedError{k}
		}
		if len(v) != 1 {
			return errors.New("ssh client: invalid scalar directive")
		}
		if want, ok := inert[k]; ok {
			if strings.ToLower(v[0]) != want {
				return &UnsupportedError{k}
			}
			continue
		}
		if bools[k] {
			v[0] = strings.ToLower(v[0])
			if v[0] != "yes" && v[0] != "no" {
				return errors.New("ssh client: invalid boolean directive")
			}
			continue
		}
		if !scalar[k] {
			return &UnsupportedError{k}
		}
		if k == "stricthostkeychecking" {
			v[0] = strings.ToLower(v[0])
			if v[0] != "ask" && v[0] != "yes" {
				return &UnsupportedError{k}
			}
		}
		if k == "connecttimeout" {
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 0 || n > 300 {
				return errors.New("ssh client: ConnectTimeout must be 0..300 seconds")
			}
		}
	}
	for _, p := range r.sendEnv {
		p = strings.TrimPrefix(p, "-")
		if p != "LANG" && p != "LC_*" && !strings.HasPrefix(p, "LC_") {
			return &UnsupportedError{"SendEnv"}
		}
	}
	return nil
}
