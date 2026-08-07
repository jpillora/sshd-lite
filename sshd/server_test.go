package sshd_test

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
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
