//go:build !windows

package scenario_test

import (
	"slices"
	"testing"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

func TestEveryEmbeddedFixtureExecutes(t *testing.T) {
	// These groups make platform assumptions explicit. The exec fixtures use
	// POSIX shell syntax; the interactive fixtures additionally require a Unix
	// PTY. Each fixture gets a fresh environment so shell and exec state cannot
	// leak between them.
	groups := map[string][]string{
		"portable-connection": {"auth_events", "connection", "echo", "multi_command"},
		"unix-exec":           {"env_vars", "exit_codes", "stderr"},
		"unix-interactive":    {"pty_resize", "shell_basic", "shell_keys"},
	}
	want, err := scenario.ListFixtures()
	if err != nil {
		t.Fatalf("list embedded fixtures: %v", err)
	}
	slices.Sort(want)
	var accounted []string
	for _, fixtures := range groups {
		accounted = append(accounted, fixtures...)
	}
	slices.Sort(accounted)
	if !slices.Equal(accounted, want) {
		t.Fatalf("fixture accounting = %v, embedded = %v", accounted, want)
	}

	for group, fixtures := range groups {
		for _, fixture := range fixtures {
			fixture := fixture
			t.Run(group+"/"+fixture, func(t *testing.T) {
				sc, err := scenario.LoadFixture(fixture)
				if err != nil {
					t.Fatalf("load fixture %q: %v", fixture, err)
				}
				env := sshtest.New(t).
					WithServer().
					WithClient("test", sshtest.ClientWithKeySeed("test")).
					Start()
				t.Cleanup(env.Stop)
				if err := env.Run(sc); err != nil {
					t.Fatalf("execute fixture %q: %v", fixture, err)
				}
			})
		}
	}
}
