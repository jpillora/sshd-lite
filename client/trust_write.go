package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// TrustWriteError reports whether the accepted key was published before a late
// persistence failure. A later authentication error never rolls trust back.
type TrustWriteError struct {
	Published bool
	Err       error
}

func (e *TrustWriteError) Error() string {
	return fmt.Sprintf("ssh client: save known_hosts (published=%t): %v", e.Published, e.Err)
}
func (e *TrustWriteError) Unwrap() error { return e.Err }

func saveHostKey(ctx context.Context, o DialOptions, address string, remote net.Addr, key ssh.PublicKey) (result error) {
	published := false
	defer func() {
		if result != nil {
			result = &TrustWriteError{published, result}
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	name := o.resolved.writeTrust
	if name == "" {
		return errors.New("no writable user known_hosts source")
	}
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	parent, err := os.Stat(dir)
	if err != nil {
		return err
	}
	lockName := name + ".lock"
	lock, err := os.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	lockInfo, err := lock.Stat()
	if err != nil {
		lock.Close()
		return err
	}
	defer func() {
		_ = lock.Close()
		if info, e := os.Lstat(lockName); e == nil && os.SameFile(info, lockInfo) {
			_ = os.Remove(lockName)
		}
	}()
	before, err := os.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if before != nil && !before.Mode().IsRegular() {
		return errors.New("refusing to replace nonregular or symlinked known_hosts")
	}
	raw, err := readSource(ctx, name, 4<<20)
	if err != nil {
		return err
	}
	// Recheck every selected trust source after locking: another connection may
	// have trusted or revoked the host while this user was considering the prompt.
	check, err := trustSnapshot(ctx, o.resolved.known, address)
	if err != nil {
		return err
	}
	err = check(address, remote, key)
	if err == nil {
		return nil
	}
	var unknown *knownhosts.KeyError
	if !errors.As(err, &unknown) || len(unknown.Want) > 0 {
		return fmt.Errorf("%w: trust changed during confirmation", ErrHostKeyRejected)
	}
	host := knownhosts.Normalize(address)
	if o.resolved.hash {
		host = knownhosts.HashHostname(host)
	}
	line := []byte(knownhosts.Line([]string{host}, key) + "\n")
	if len(raw)+len(line)+1 > 4<<20 {
		return errors.New("known_hosts byte limit")
	}
	content := append([]byte(nil), raw...)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, line...)
	mode := os.FileMode(0600)
	if before != nil {
		mode = before.Mode().Perm()
	}
	if err := lock.Chmod(mode); err != nil {
		return err
	}
	if _, err := lock.Write(content); err != nil {
		return err
	}
	if err := lock.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := os.Lstat(name)
	if before == nil {
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("known_hosts appeared concurrently")
		}
	} else if err != nil || !os.SameFile(before, current) || before.Mode() != current.Mode() {
		return errors.New("known_hosts replaced concurrently")
	}
	latest, err := readSource(ctx, name, 4<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(latest, raw) {
		return errors.New("known_hosts modified concurrently")
	}
	now, err := os.Stat(dir)
	if err != nil || !os.SameFile(parent, now) {
		return errors.New("known_hosts directory replaced")
	}
	owned, err := os.Lstat(lockName)
	if err != nil || !os.SameFile(owned, lockInfo) {
		return errors.New("known_hosts lock ownership lost")
	}
	if err := lock.Close(); err != nil {
		return err
	}
	if err := os.Rename(lockName, name); err != nil {
		return err
	}
	published = true
	// Go's Windows directory handles do not support Sync. The file itself
	// was synced above; parent-directory durability is only available on Unix.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
