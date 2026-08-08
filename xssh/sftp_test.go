package xssh

import (
	"errors"
	"io"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

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
