package xssh

import (
	"path/filepath"
	"testing"
)

// TestNewConnDoesNotMutateConfig guards the invariant that makes per-connection
// defaulting safe: servers hand the same *Config to every NewConn, so resolving
// the shell path in place would race across concurrent connections.
func TestNewConnDoesNotMutateConfig(t *testing.T) {
	// Session enabled with an unset Shell is what triggers the resolution.
	shared := &Config{Session: true}
	first := NewConn(nil, nil, nil, shared)
	second := NewConn(nil, nil, nil, shared)

	if shared.Shell != "" {
		t.Fatalf("NewConn wrote to the caller's Config.Shell: %q", shared.Shell)
	}
	resolved := first.Config().Shell
	if resolved == "" {
		t.Skip("no shell on this machine, nothing was resolved")
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("Shell not resolved to an absolute path: %q", resolved)
	}
	if got := second.Config().Shell; got != resolved {
		t.Fatalf("connections disagree on shell: %q vs %q", resolved, got)
	}
}
