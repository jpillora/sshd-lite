package xssh

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBaseEnvOptOutFiltersProcessEnvironment(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_SECRET", "hunter2")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "should-not-escape")

	env := baseEnv(true)
	for _, kv := range env {
		name := strings.SplitN(kv, "=", 2)[0]
		if !slices.Contains(baseEnvNames, name) {
			t.Errorf("baseEnv leaked %q, which is not in baseEnvNames", name)
		}
	}
	if hasEnv(env, "SSHD_LITE_TEST_SECRET") {
		t.Error("baseEnv leaked SSHD_LITE_TEST_SECRET into the session environment")
	}
	if hasEnv(env, "AWS_SECRET_ACCESS_KEY") {
		t.Error("baseEnv leaked AWS_SECRET_ACCESS_KEY into the session environment")
	}
}

func TestBaseEnvKeepsWhatAShellNeeds(t *testing.T) {
	// PATH is the one variable a session is unusable without. Windows spells it
	// Path in baseEnvNames, so the lookup has to be name-case agnostic the same
	// way the platform is.
	t.Setenv("PATH", "/usr/bin:/bin")

	env := baseEnv(true)
	if !hasEnv(env, "PATH") {
		t.Fatalf("baseEnv dropped PATH: %v", env)
	}
	for _, kv := range env {
		if envKeyMatches(kv, "PATH") && kv[len("PATH="):] != "/usr/bin:/bin" {
			t.Errorf("baseEnv rewrote PATH: got %q", kv)
		}
	}
}

// Windows environment names are case-insensitive, so a client's PATH must
// replace an inherited Path instead of sitting beside it and leaving the winner
// to chance.
func TestAppendEnvHonoursPlatformNameCasing(t *testing.T) {
	env := appendEnv([]string{"Path=C:\\windows", "HOME=/home/u"}, "PATH=/override")
	if envNamesCaseInsensitive {
		if len(env) != 2 {
			t.Errorf("appendEnv added a duplicate name: %q", env)
		}
		if !slices.Contains(env, "PATH=/override") {
			t.Errorf("appendEnv did not replace Path: %q", env)
		}
	} else {
		if len(env) != 3 {
			t.Errorf("appendEnv should treat Path and PATH as distinct on unix: %q", env)
		}
	}
	// Exact-name replacement works on every platform.
	env = appendEnv([]string{"HOME=/home/u"}, "HOME=/root")
	if !slices.Equal(env, []string{"HOME=/root"}) {
		t.Errorf("appendEnv() = %q, want [HOME=/root]", env)
	}
	// A prefix of another name must not be mistaken for it.
	env = appendEnv([]string{"HOMEBREW=/opt"}, "HOME=/root")
	if len(env) != 2 {
		t.Errorf("appendEnv confused HOME with HOMEBREW: %q", env)
	}
}

func TestBaseEnvInheritReturnsProcessEnvironment(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_SECRET", "hunter2")

	env := baseEnv(false)
	if !hasEnv(env, "SSHD_LITE_TEST_SECRET") {
		t.Error("baseEnv(false) should return the process environment verbatim")
	}
	if len(env) != len(os.Environ()) {
		t.Errorf("baseEnv(false) returned %d vars, want %d", len(env), len(os.Environ()))
	}
}

func TestReadEnvironmentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment")
	data := `
# system defaults
PATH = "/usr/local/bin:/usr/bin"
SINGLE='hello # world' # comment
DOUBLE="hello world"
PLAIN=hello world # comment
EMPTY=
export EXPORTED=yes
EQUALS=a=b
LITERAL=$HOME:$(echo untouched)` + "`echo untouched`\n" + `
DUPLICATE=old
DUPLICATE=new
INVALID LINE
=empty-name
1BAD=value
BAD NAME=value
UNTERMINATED="value
TRAILING="value"junk
` + "NUL=bad\x00value\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readEnvironmentFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATH=/usr/local/bin:/usr/bin", "SINGLE=hello # world", "DOUBLE=hello world",
		"PLAIN=hello world", "EMPTY=", "EXPORTED=yes", "EQUALS=a=b",
		"LITERAL=$HOME:$(echo untouched)`echo untouched`", "DUPLICATE=new",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

func TestSessionEnvironmentPrecedence(t *testing.T) {
	const name = "SSHD_LITE_TEST_INHERITED"
	t.Setenv(name, "process")
	t.Setenv("SSHD_LITE_TEST_EMPTY", "")
	path := filepath.Join(t.TempDir(), "environment")
	if err := os.WriteFile(path, []byte(name+"=system\nSSHD_LITE_TEST_SYSTEM_ONLY=system\nSSHD_LITE_TEST_EMPTY=system\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, noInherit := range []bool{false, true} {
		env, err := sessionEnv(noInherit, false, path)
		if err != nil {
			t.Fatal(err)
		}
		want := name + "=process"
		empty := "SSHD_LITE_TEST_EMPTY="
		if noInherit {
			want = name + "=system"
			empty += "system"
		}
		for _, kv := range []string{want, empty, "SSHD_LITE_TEST_SYSTEM_ONLY=system"} {
			if !slices.Contains(env, kv) {
				t.Errorf("NoInheritEnv=%v: missing %q", noInherit, kv)
			}
		}
		if got := os.Getenv(name); got != "process" {
			t.Fatalf("session changed process environment to %q", got)
		}
	}
}

func TestSessionEnvironmentMissingOrUnreadableFile(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_INHERITED", "process")
	dir := t.TempDir()
	for _, path := range []string{"", filepath.Join(dir, "missing"), dir} {
		env, err := sessionEnv(false, false, path)
		if (err != nil) != (path == dir) {
			t.Errorf("sessionEnv(%q) error = %v", path, err)
		}
		if !slices.Contains(env, "SSHD_LITE_TEST_INHERITED=process") {
			t.Errorf("sessionEnv(%q) lost inherited variable", path)
		}
	}
}

func TestSessionEnvironmentNoGlobalEnv(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_INHERITED", "process")
	dir := t.TempDir()
	path := filepath.Join(dir, "environment")
	if err := os.WriteFile(path, []byte("SSHD_LITE_TEST_GLOBAL=system\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, noInherit := range []bool{false, true} {
		for _, noGlobal := range []bool{false, true} {
			env, err := sessionEnv(noInherit, noGlobal, path)
			if err != nil {
				t.Fatal(err)
			}
			if hasEnv(env, "SSHD_LITE_TEST_GLOBAL") == noGlobal {
				t.Errorf("NoInheritEnv=%v, NoGlobalEnv=%v: unexpected global environment", noInherit, noGlobal)
			}
			if hasEnv(env, "SSHD_LITE_TEST_INHERITED") == noInherit {
				t.Errorf("NoInheritEnv=%v, NoGlobalEnv=%v: unexpected process inheritance", noInherit, noGlobal)
			}
		}
	}
	// A disabled global environment must not attempt to read the path.
	if _, err := sessionEnv(false, true, dir); err != nil {
		t.Fatalf("NoGlobalEnv attempted to read system environment: %v", err)
	}
}

func TestSessionEnvironmentOptOutWithNoEssentialVariables(t *testing.T) {
	for _, name := range baseEnvNames {
		// Register restoration before removing each essential variable.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	env, err := sessionEnv(true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// Filtering to the platform's essential names is covered by the baseEnv
	// tests. The invariant here is non-nil: exec.Cmd interprets nil as a request
	// to inherit the complete environment.
	if env == nil {
		t.Fatal("empty opt-out environment must be non-nil to prevent exec.Cmd inheritance")
	}
}

func TestConnectionEnvMatchesOpenSSHFormat(t *testing.T) {
	remote := &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 51234}
	local := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}

	env := connectionEnv(remote, local)
	want := []string{
		"SSH_CLIENT=10.0.0.5 51234 22",
		"SSH_CONNECTION=10.0.0.5 51234 10.0.0.1 22",
	}
	if !slices.Equal(env, want) {
		t.Errorf("connectionEnv() = %q, want %q", env, want)
	}
}

// A half-formed SSH_CLIENT is worse than none: scripts parse it positionally.
func TestConnectionEnvOmittedWhenAddressUnusable(t *testing.T) {
	good := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 22}
	for _, tt := range []struct {
		name          string
		remote, local net.Addr
	}{
		{"nil remote", nil, good},
		{"nil local", good, nil},
		{"both nil", nil, nil},
		{"unix socket", &net.UnixAddr{Name: "/tmp/s", Net: "unix"}, good},
	} {
		if env := connectionEnv(tt.remote, tt.local); env != nil {
			t.Errorf("%s: connectionEnv() = %q, want nil", tt.name, env)
		}
	}
}
