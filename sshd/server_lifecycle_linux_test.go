//go:build linux

package sshd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartWithContextCancellationWaitsForCommandCleanup(t *testing.T) {
	server := newLifecycleTestServer(t, Config{})
	listener := listenLifecycleTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.StartWithContext(ctx, listener) }()

	client := dialLifecycleTest(t, listener.Addr().String())
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		cancel()
		t.Fatalf("new command session: %v", err)
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("command stdout: %v", err)
	}
	if err := session.Start(`printf '%d\n' $$; exec sleep 300`); err != nil {
		cancel()
		t.Fatalf("start long command: %v", err)
	}
	type readResult struct {
		line string
		err  error
	}
	pidResult := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		pidResult <- readResult{line: line, err: err}
	}()
	var line string
	select {
	case read := <-pidResult:
		if read.err != nil {
			cancel()
			t.Fatalf("read command PID: %v", read.err)
		}
		line = read.line
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for command PID")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		cancel()
		t.Fatalf("invalid command PID %q: %v", line, err)
	}
	startTime, err := lifecycleLinuxProcessStartTime(pid)
	if err != nil {
		cancel()
		t.Fatalf("capture command identity: %v", err)
	}
	t.Cleanup(func() {
		if current, err := lifecycleLinuxProcessStartTime(pid); err == nil && current == startTime {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StartWithContext returned an error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartWithContext did not wait for command cleanup")
	}
	if current, err := lifecycleLinuxProcessStartTime(pid); err == nil && current == startTime {
		t.Fatalf("StartWithContext returned while command process %d was still alive", pid)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe command process after shutdown: %v", err)
	}
}

func lifecycleLinuxProcessStartTime(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	closing := strings.LastIndexByte(string(data), ')')
	if closing < 0 {
		return "", fmt.Errorf("malformed stat data")
	}
	fields := strings.Fields(string(data)[closing+1:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("short stat data")
	}
	return fields[19], nil
}
