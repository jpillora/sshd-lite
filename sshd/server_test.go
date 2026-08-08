package sshd_test

import (
	"fmt"
	"io"
	"net"
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
