package sshd

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestServerLogsResolvedWorkDirAndSSHListener(t *testing.T) {
	dir := t.TempDir()
	var logs lockedLogBuffer
	server, err := NewServer(Config{
		AuthType:  "none",
		KeySeed:   "logging-test",
		KeySeedEC: true,
		WorkDir:   dir,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Work directory: "+dir) {
		t.Fatalf("work directory log missing: %s", logs.String())
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.StartWithContext(ctx, listener) }()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "Listening on tcp://"+listener.Addr().String()+" for ssh connections") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Listening on tcp://"+listener.Addr().String()+" for ssh connections") {
		t.Fatalf("listener log missing: %s", logs.String())
	}
}
