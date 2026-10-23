package client

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Legacy trust publication retains replacement, wildcard and symlink semantics.
// Dial's guarded unknown-key publication is intentionally separate in trust_write.go.
func updateKnownHosts(filename, hostname string, key ssh.PublicKey, stale []knownhosts.KnownKey) error {
	return updateKnownHostsEntry(filename, hostname, key, stale, "")
}

func updateKnownHostsEntry(filename, hostname string, key ssh.PublicKey, stale []knownhosts.KnownKey, marker string) error {
	configuredName := filename
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		filename, err = filepath.EvalSymlinks(filename)
		if err != nil {
			return fmt.Errorf("resolve known hosts symlink: %w", err)
		}
	}
	data, err := os.ReadFile(filename)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read known hosts: %w", err)
	}
	mode := fs.FileMode(0o600)
	if info, statErr := os.Stat(filename); statErr == nil {
		mode = info.Mode().Perm()
	}
	remove := make(map[int]struct{})
	for _, known := range stale {
		if sameFile(known.Filename, configuredName) || sameFile(known.Filename, filename) {
			remove[known.Line] = struct{}{}
		}
	}
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		if _, drop := remove[i+1]; drop {
			if remainder, ok := preserveOtherHosts(line, hostname); ok {
				kept = append(kept, remainder)
			}
			continue
		}
		if i == len(lines)-1 && line == "" {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept, marker+knownhosts.Line([]string{hostname}, key))
	content := []byte(strings.Join(kept, "\n") + "\n")

	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create known hosts directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".known-hosts-*")
	if err != nil {
		return fmt.Errorf("create known hosts update: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write known hosts update: %w", err)
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("replace known hosts: %w", err)
	}
	return nil
}

// A known_hosts line can bind one key to several comma-separated hosts. When
// the changed host is an explicit member, retain the other bindings. Hashed and
// wildcard matches retain their other bindings by excluding the changed host.
// Hashed matches cannot be separated safely, so those lines are removed before
// the replacement is appended.
func preserveOtherHosts(line, hostname string) (string, bool) {
	fields := strings.Fields(line)
	hostField := 0
	if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
		hostField = 1
	}
	if len(fields) <= hostField {
		return "", false
	}
	target := knownhosts.Normalize(hostname)
	hosts := strings.Split(fields[hostField], ",")
	if strings.HasPrefix(fields[hostField], "|") {
		return "", false
	}
	remaining := hosts[:0]
	removed := false
	for _, host := range hosts {
		if host == target || host == hostname {
			removed = true
			continue
		}
		remaining = append(remaining, host)
	}
	if removed && len(remaining) == 0 {
		return "", false
	}
	// The stale key may still match through a remaining wildcard. An explicit
	// negation ensures the replacement wins while retaining every other binding.
	remaining = append([]string{"!" + target}, remaining...)
	fields[hostField] = strings.Join(remaining, ",")
	return strings.Join(fields, " "), true
}

func sameFile(a, b string) bool {
	if aa, errA := os.Stat(a); errA == nil {
		if bb, errB := os.Stat(b); errB == nil {
			return os.SameFile(aa, bb)
		}
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
