package sshtest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileFailurePreservesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "destination")
	want := []byte("existing destination")
	if err := os.WriteFile(destination, want, 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	if err := replaceFile(filepath.Join(dir, "missing-source"), destination); err == nil {
		t.Fatal("replace with missing source unexpectedly succeeded")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination after failed replace: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("destination after failed replace = %q, want %q", got, want)
	}
}
