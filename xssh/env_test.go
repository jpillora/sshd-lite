package xssh

import (
	"net"
	"os"
	"slices"
	"strings"
	"testing"
)

// The server runs shells as whoever started it, so an inherited environment is
// handed wholesale to every authenticated client. This is the guard against
// that regressing.
func TestBaseEnvDoesNotLeakServerEnvironment(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_SECRET", "hunter2")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "should-not-escape")

	env := baseEnv(false)
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

	env := baseEnv(false)
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

	env := baseEnv(true)
	if !hasEnv(env, "SSHD_LITE_TEST_SECRET") {
		t.Error("baseEnv(true) should return the process environment verbatim")
	}
	if len(env) != len(os.Environ()) {
		t.Errorf("baseEnv(true) returned %d vars, want %d", len(env), len(os.Environ()))
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
