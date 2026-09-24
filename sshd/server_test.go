package sshd_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"golang.org/x/crypto/ssh"
)

type testCase struct {
	name    string
	options []sshtest.ServerOption
	client  func(addr string) error
}

func TestAll(t *testing.T) {
	t.Parallel()
	for i, tc := range []testCase{
		tcpCheck,
		exec,
		tcpForwardingLocal,
		tcpForwardingRemote,
	} {
		t.Run(fmt.Sprintf("#%d-%s", i+1, tc.name), func(t *testing.T) {
			// Create and start test server
			server, err := sshtest.NewServer(tc.options...)
			if err != nil {
				t.Fatalf("Failed to create server: %v", err)
			}

			if err := server.Start(t.Context()); err != nil {
				t.Fatalf("Failed to start server: %v", err)
			}
			defer server.Stop()

			addr := server.Addr()
			t.Logf("Test server listening: %s", addr)

			// Run client test
			if err := tc.client(addr); err != nil {
				t.Errorf("Test case failed: %v", err)
			} else {
				t.Log("Test case passed")
			}
		})
	}
}

// TestExecStdin checks that a command reading from stdin sees EOF once the
// client half-closes the channel. "cat" never exits otherwise, so a regression
// here shows up as a hang rather than a wrong result.
func TestExecStdin(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("cat not available on Windows")
	}
	server, err := sshtest.NewServer(sshtest.ServerWithNoAuth())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	c, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	s.Stdin = strings.NewReader("helloworld\n")

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := s.Output("cat")
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Failed to run command: %v", r.err)
		}
		if got := strings.ReplaceAll(string(r.out), "\r\n", "\n"); got != "helloworld\n" {
			t.Fatalf("Unexpected output: %q", r.out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for command to exit, stdin was never closed")
	}
}

func TestExecWithRequestedPTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	client := startExecTestServer(t)
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	if err := session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}
	session.Stdin = strings.NewReader("hello\n")
	out, err := session.CombinedOutput("test -t 0 && test -t 1 && read value && printf 'terminal:%s:%s\\n' \"$TERM\" \"$value\"")
	if err != nil {
		t.Fatalf("run command with pty: %v; output: %q", err, out)
	}
	if !strings.Contains(string(out), "terminal:xterm-256color:hello") {
		t.Fatalf("command output = %q", out)
	}

	failing, err := client.NewSession()
	if err != nil {
		t.Fatalf("new failing session: %v", err)
	}
	defer failing.Close()
	if err := failing.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatalf("request failing command pty: %v", err)
	}
	exitErr, ok := failing.Run("exit 7").(*ssh.ExitError)
	if !ok || exitErr.ExitStatus() != 7 {
		t.Fatalf("pty command exit status = %v, want 7", exitErr)
	}
}

func TestExecSeparatesStdoutAndStderr(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t)

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	command := "printf 'stdout-only'; printf 'stderr-only' >&2"
	if runtime.GOOS == "windows" {
		command = "[Console]::Out.Write('stdout-only'); [Console]::Error.Write('stderr-only')"
	}
	if err := session.Run(command); err != nil {
		t.Fatalf("run command: %v", err)
	}
	if got := stdout.String(); got != "stdout-only" {
		t.Fatalf("stdout = %q, want %q", got, "stdout-only")
	}
	if got := stderr.String(); got != "stderr-only" {
		t.Fatalf("stderr = %q, want %q", got, "stderr-only")
	}
}

func TestExecSetupFailureReportsStderrAndNonzeroStatus(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	server, err := sshtest.NewServer(
		sshtest.ServerWithNoAuth(),
		sshtest.ServerWithWorkDir(missing),
	)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()

	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run("echo unreachable")
	exitErr, ok := err.(*ssh.ExitError)
	if !ok {
		t.Fatalf("run error = %T %v, want *ssh.ExitError", err, err)
	}
	if exitErr.ExitStatus() == 0 {
		t.Fatal("setup failure returned exit status 0")
	}
	if stdout.Len() != 0 {
		t.Fatalf("setup failure wrote stdout: %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "failed to start command") || !strings.Contains(got, missing) {
		t.Fatalf("setup failure stderr = %q, want action and working directory", got)
	}
}

func TestExecPreservesExitStatus(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t)
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	err = session.Run("exit 37")
	exitErr, ok := err.(*ssh.ExitError)
	if !ok {
		t.Fatalf("run error = %T %v, want *ssh.ExitError", err, err)
	}
	if got := exitErr.ExitStatus(); got != 37 {
		t.Fatalf("exit status = %d, want 37", got)
	}
}

func startExecTestServer(t *testing.T) *ssh.Client {
	t.Helper()
	server, err := sshtest.NewServer(sshtest.ServerWithNoAuth())
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestMalformedResizeAfterShellKeepsConnectionUsable exercises the wire-level
// failure path after a process has started. The malformed request must fail
// without killing the shell dispatcher, SSH connection, or daemon.
func TestMalformedResizeAfterShellKeepsConnectionUsable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test harness shell command is Unix-specific")
	}
	server, err := sshtest.NewServer(sshtest.ServerWithNoAuth())
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()

	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	shell, err := client.NewSession()
	if err != nil {
		t.Fatalf("new shell session: %v", err)
	}
	defer shell.Close()
	shell.Stdout = io.Discard
	shell.Stderr = io.Discard
	stdin, err := shell.StdinPipe()
	if err != nil {
		t.Fatalf("open shell stdin: %v", err)
	}
	defer stdin.Close()
	if err := shell.RequestPty("xterm", 24, 80, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		t.Fatalf("request PTY: %v", err)
	}
	if err := shell.Shell(); err != nil {
		t.Fatalf("start shell: %v", err)
	}

	ok, err := shell.SendRequest("window-change", true, []byte{0, 0, 0})
	if err != nil {
		t.Fatalf("send malformed resize: %v", err)
	}
	if ok {
		t.Fatal("malformed resize was accepted")
	}

	validResize := ssh.Marshal(&struct {
		Columns, Rows           uint32
		PixelWidth, PixelHeight uint32
	}{Columns: 100, Rows: 30, PixelWidth: 800, PixelHeight: 480})
	ok, err = shell.SendRequest("window-change", true, validResize)
	if err != nil {
		t.Fatalf("send valid resize after malformed resize: %v", err)
	}
	if !ok {
		t.Fatal("valid resize after malformed resize was rejected")
	}

	later, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session after malformed resize: %v", err)
	}
	defer later.Close()
	out, err := later.CombinedOutput("echo still-alive")
	if err != nil {
		t.Fatalf("exec after malformed resize: %v", err)
	}
	if !strings.Contains(string(out), "still-alive") {
		t.Fatalf("unexpected later session output: %q", out)
	}
}

var tcpCheck = testCase{
	name:    "tcp-check",
	options: []sshtest.ServerOption{sshtest.ServerWithNoAuth()},
	client: func(addr string) error {
		// Test that we can connect to the port
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
		}
		return err
	},
}

var exec = testCase{
	name:    "exec",
	options: []sshtest.ServerOption{sshtest.ServerWithNoAuth()},
	client: func(addr string) error {
		c, err := sshtest.CreateSSHClient(addr)
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer c.Close()
		s, err := c.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
		out, err := s.CombinedOutput("echo helloworld")
		if err != nil {
			return fmt.Errorf("failed to run command: %w", err)
		}
		cleanOut := strings.ReplaceAll(string(out), "\r\n", "\n")
		if cleanOut != "helloworld\n" {
			return fmt.Errorf("unexpected output: %q", out)
		}
		return nil
	},
}
