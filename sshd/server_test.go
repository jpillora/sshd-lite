package sshd_test

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
)

type testCase struct {
	name   string
	server *sshd.Config
	client func(addr string) error
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
			// test server
			addr, serverDone := newTestServer(t.Context(), tc.server)
			t.Logf("Test server listening: %s", addr)
			// test client
			clientDone := make(chan error)
			go func() {
				clientDone <- tc.client(addr)
			}()
			// Wait for server to stop or timeout
			select {
			case err := <-serverDone:
				// Server should stop cleanly when listener is closed
				if err != nil {
					t.Logf("Server stopped with: %v", err)
				}
			case err := <-clientDone:
				if err != nil {
					t.Errorf("Test case failed: %v", err)
				} else {
					t.Log("Test case passed")
				}
			}
		})
	}
}

var tcpCheck = testCase{
	name:   "tcp-check",
	server: &sshd.Config{},
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
	name: "exec",
	server: &sshd.Config{
		LogVerbose: true,
	},
	client: func(addr string) error {
		c, err := createSSHClient(addr)
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
