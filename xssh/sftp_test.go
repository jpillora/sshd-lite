package xssh

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sftp"
	"golang.org/x/crypto/ssh"
)

func TestRootedSFTPOperationsStayWithinRoot(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkAvailable := os.Symlink(outsideDir, filepath.Join(rootDir, "escape")) == nil

	serverConn, clientConn := net.Pipe()
	server, err := newRootedSFTPServer(serverConn, rootDir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		server.CloseRoot()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("rooted SFTP server did not stop")
		}
	})

	if err := client.Mkdir("dir"); err != nil {
		t.Fatal(err)
	}
	file, err := client.Create("/dir/file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("contents")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Chmod("dir/file", 0o640); err != nil {
		t.Fatal(err)
	}
	if err := client.Truncate("dir/file", 4); err != nil {
		t.Fatal(err)
	}
	if err := client.Rename("dir/file", "dir/renamed"); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 0)
	if err := client.Chtimes("dir/renamed", mtime.Add(-time.Hour), mtime); err != nil {
		t.Fatal(err)
	}
	if err := client.Link("dir/renamed", "dir/hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := client.Symlink("/dir/renamed", "dir/symlink"); err != nil {
		t.Fatal(err)
	}
	if got, err := client.ReadLink("dir/symlink"); err != nil || got != "renamed" {
		t.Fatalf("readlink = %q, %v", got, err)
	}
	if got, err := client.RealPath("../../dir/renamed"); err != nil || got != "/dir/renamed" {
		t.Fatalf("realpath = %q, %v", got, err)
	}
	entries, err := client.ReadDir("dir")
	if err != nil || len(entries) != 3 {
		t.Fatalf("readdir = %v, %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(rootDir, "dir", "renamed"))
	if err != nil || string(data) != "cont" {
		t.Fatalf("rooted file = %q, %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(rootDir, "dir", "renamed")); err != nil || info.ModTime().Unix() != mtime.Unix() {
		t.Fatalf("rooted file mtime = %v, %v", info, err)
	}
	if symlinkAvailable {
		if _, err := client.Open("escape/outside"); err == nil {
			t.Fatal("opened a file through an escaping symlink")
		}
		if file, err := client.Create("escape/created"); err == nil {
			file.Close()
			t.Fatal("created a file through an escaping symlink")
		}
		outsideInfo, err := os.Stat(outside)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Chmod("escape/outside", 0o777); err == nil {
			t.Fatal("changed metadata through an escaping symlink")
		}
		after, err := os.Stat(outside)
		if err != nil || after.Mode().Perm() != outsideInfo.Mode().Perm() {
			t.Fatalf("outside metadata changed: before=%v after=%v err=%v", outsideInfo.Mode(), after, err)
		}
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}

func TestRootedSFTPHandleMetadataSurvivesRenameAndReplacement(t *testing.T) {
	rootDir := t.TempDir()
	serverConn, clientConn := net.Pipe()
	server, err := newRootedSFTPServer(serverConn, rootDir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		server.CloseRoot()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("rooted SFTP server did not stop")
		}
	})

	file, err := client.OpenFile("original", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("original contents")); err != nil {
		t.Fatal(err)
	}
	if err := client.Rename("original", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "original"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if info, err := file.Stat(); err != nil || info.Size() != int64(len("original contents")) {
		t.Fatalf("open handle stat after rename = %v, %v", info, err)
	}
	if err := file.Truncate(4); err != nil {
		t.Fatalf("truncate open handle after rename: %v", err)
	}
	if err := file.Chmod(0o640); err != nil {
		t.Fatalf("chmod open handle after rename: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(rootDir, "renamed")); err != nil || string(data) != "orig" {
		t.Fatalf("renamed open file = %q, %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(rootDir, "renamed")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("renamed open file mode = %v, %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(rootDir, "original")); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement file was changed = %q, %v", data, err)
	}
}

func TestSFTPServeFailureClosesChannelAndReportsFailure(t *testing.T) {
	channel := &failingSFTPChannel{readErr: errors.New("injected SFTP transport failure")}
	startSFTPServer(&Session{Channel: channel}, SFTPConfig{WorkDir: t.TempDir()})

	channel.mu.Lock()
	defer channel.mu.Unlock()
	if !channel.closed {
		t.Fatal("SFTP channel was not closed after Serve failure")
	}
	if channel.requestType != "exit-status" {
		t.Fatalf("final request type = %q, want exit-status", channel.requestType)
	}
	var status struct{ Status uint32 }
	if err := ssh.Unmarshal(channel.requestPayload, &status); err != nil {
		t.Fatalf("decode exit status: %v", err)
	}
	if status.Status != 1 {
		t.Fatalf("SFTP Serve failure exit status = %d, want 1", status.Status)
	}
}

type failingSFTPChannel struct {
	mu             sync.Mutex
	readErr        error
	closed         bool
	requestType    string
	requestPayload []byte
}

func (c *failingSFTPChannel) Read([]byte) (int, error) { return 0, c.readErr }
func (c *failingSFTPChannel) Write(p []byte) (int, error) {
	return len(p), nil
}
func (c *failingSFTPChannel) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}
func (c *failingSFTPChannel) CloseWrite() error { return nil }
func (c *failingSFTPChannel) SendRequest(name string, _ bool, payload []byte) (bool, error) {
	c.mu.Lock()
	c.requestType = name
	c.requestPayload = append([]byte(nil), payload...)
	c.mu.Unlock()
	return true, nil
}
func (c *failingSFTPChannel) Stderr() io.ReadWriter { return controlledReadWriter{} }
