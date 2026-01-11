package sshd_test

import (
	"fmt"
	"net"
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
