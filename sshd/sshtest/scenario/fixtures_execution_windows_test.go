//go:build windows

package scenario_test

import (
	"slices"
	"testing"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

func TestEmbeddedFixtureWindowsAccounting(t *testing.T) {
	portable := []string{"auth_events", "connection", "echo", "multi_command"}
	unixOnly := []string{"env_vars", "exit_codes", "pty_resize", "shell_basic", "shell_keys", "stderr"}
	all, err := scenario.ListFixtures()
	if err != nil {
		t.Fatalf("list embedded fixtures: %v", err)
	}
	accounted := append(append([]string{}, portable...), unixOnly...)
	slices.Sort(accounted)
	slices.Sort(all)
	if !slices.Equal(accounted, all) {
		t.Fatalf("fixture accounting = %v, embedded = %v", accounted, all)
	}
	for _, fixture := range portable {
		t.Run(fixture, func(t *testing.T) {
			sc, err := scenario.LoadFixture(fixture)
			if err != nil {
				t.Fatalf("load fixture %q: %v", fixture, err)
			}
			env := sshtest.New(t).WithServer().WithClient("test", sshtest.ClientWithKeySeed("test")).Start()
			t.Cleanup(env.Stop)
			if err := env.Run(sc); err != nil {
				t.Fatalf("execute fixture %q: %v", fixture, err)
			}
		})
	}
	for _, fixture := range unixOnly {
		t.Run(fixture, func(t *testing.T) {
			t.Skip("fixture uses POSIX shell or Unix PTY semantics; covered by fixtures_execution_unix_test.go")
		})
	}
}
