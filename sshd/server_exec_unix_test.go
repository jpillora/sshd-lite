//go:build linux

package sshd_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestExecDisconnectTerminatesCommandAndKeepsConnectionUsable(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t)
	processes := startPIDCommand(t, client, `printf '%d\n' $$; exec sleep 300`)

	assertSessionStillUsable(t, client)
	waitForProcessExit(t, processes[0])
}

func TestExecDisconnectTerminatesDescendants(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t)
	processes := startPIDCommand(t, client, `sleep 300 & child=$!; printf '%d %d\n' "$$" "$child"; wait "$child"`)
	waitForProcessExit(t, processes[0])
	waitForProcessExit(t, processes[1])
}

type linuxProcessIdentity struct {
	pid       int
	startTime string
}

func startPIDCommand(t *testing.T, client *ssh.Client, command string) []linuxProcessIdentity {
	t.Helper()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := session.Start(command); err != nil {
		session.Close()
		t.Fatalf("start command: %v", err)
	}

	line := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		value, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			readErr <- err
			return
		}
		line <- value
	}()

	var value string
	select {
	case value = <-line:
	case err := <-readErr:
		session.Close()
		t.Fatalf("read command pid: %v", err)
	case <-time.After(5 * time.Second):
		session.Close()
		t.Fatal("timed out waiting for command pid")
	}
	fields := strings.Fields(value)
	processes := make([]linuxProcessIdentity, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			session.Close()
			t.Fatalf("invalid command pid %q: %v", value, err)
		}
		startTime, err := linuxProcessStartTime(pid)
		if err != nil {
			session.Close()
			t.Fatalf("capture identity for process %d: %v", pid, err)
		}
		// Register cleanup before exercising disconnect behavior. Checking the
		// kernel start time prevents a delayed cleanup from signaling a reused PID.
		identity := linuxProcessIdentity{pid: pid, startTime: startTime}
		t.Cleanup(func() { bestEffortKillLinuxProcess(identity) })
		processes = append(processes, identity)
	}
	if err := session.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("close command session: %v", err)
	}
	return processes
}

func assertSessionStillUsable(t *testing.T, client *ssh.Client) {
	t.Helper()
	later, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session after command disconnect: %v", err)
	}
	defer later.Close()
	out, err := later.Output("printf still-usable")
	if err != nil {
		t.Fatalf("run command after command disconnect: %v", err)
	}
	if got := string(out); got != "still-usable" {
		t.Fatalf("later session output = %q", got)
	}
}

func linuxProcessStartTime(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// comm is parenthesized and may contain spaces. Fields following its last
	// ')' begin at proc(5) field 3; starttime is field 22.
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

func bestEffortKillLinuxProcess(process linuxProcessIdentity) {
	current, err := linuxProcessStartTime(process.pid)
	if err != nil || current != process.startTime {
		return
	}
	_ = syscall.Kill(process.pid, syscall.SIGKILL)
}

func waitForProcessExit(t *testing.T, process linuxProcessIdentity) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := linuxProcessStartTime(process.pid)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			t.Fatalf("probe process %d: %v", process.pid, err)
		}
		if current != process.startTime {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d survived session disconnect", process.pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
