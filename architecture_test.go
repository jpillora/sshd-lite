package main

import (
	"os/exec"
	"strings"
	"testing"
)

// A disabled runtime flag is insufficient: SSH library users must not compile
// or link the optional Mosh protocol, emulator, or bootstrap shell parser.
func TestSSHDependenciesExcludeMosh(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./sshd", "./client", "./xssh").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect SSH dependencies: %v\n%s", err, out)
	}
	for _, path := range strings.Fields(string(out)) {
		for _, forbidden := range []string{"github.com/jpillora/sshd-lite/mosh", "github.com/jpillora/sshd-lite/internal/mosh", "github.com/unixshells/", "github.com/charmbracelet/", "mvdan.cc/sh/"} {
			if strings.HasPrefix(path, forbidden) {
				t.Errorf("SSH-only imports optional Mosh dependency %s", path)
			}
		}
	}
}

// The optional adapter uses the lower-level SSH session API directly; it does
// not need the high-level SSH server or SSH client package to bootstrap Mosh.
func TestMoshDependencyBoundary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./mosh").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect Mosh dependencies: %v\n%s", err, out)
	}
	for _, path := range strings.Fields(string(out)) {
		for _, forbidden := range []string{"github.com/jpillora/sshd-lite/sshd", "github.com/jpillora/sshd-lite/client"} {
			if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
				t.Errorf("Mosh imports high-level SSH package %s", path)
			}
		}
	}
}
