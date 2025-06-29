package sshd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// getRandomListener creates a listener on a random port and returns it along with the address
func getRandomListener() (net.Listener, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	addr := listener.Addr().(*net.TCPAddr)
	return listener, addr.String(), nil
}

type testCase struct {
	name   string
	server *Config
	client func(addr string) error
}

var testCases = []testCase{
	{
		name:   "tcp-check",
		server: &Config{},
		client: func(addr string) error {
			// Test that we can connect to the port
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				conn.Close()
			}
			return err
		},
	},
	{
		name: "exec",
		server: &Config{
			LogVerbose: true,
		},
		client: func(addr string) error {
			// Test that we can connect to the port
			c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				Timeout:         5 * time.Second,
			})
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
			if string(out) != "helloworld\n" {
				return fmt.Errorf("unexpected output: %q", out)
			}
			return nil
		},
	},
	{
		name: "tcp-forwarding",
		server: &Config{
			TCPForwarding: true,
			LogVerbose:    true,
		},
		client: func(addr string) error {
			// 1. Start a go http server on a random port
			httpListener, httpAddr, err := getRandomListener()
			if err != nil {
				return fmt.Errorf("failed to create http listener: %w", err)
			}
			defer httpListener.Close()

			// 2. The server should respond to GET / with "foo"
			httpServer := &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/" {
						w.Write([]byte("foo"))
					} else {
						http.NotFound(w, r)
					}
				}),
			}
			go httpServer.Serve(httpListener)
			defer httpServer.Close()

			// Connect to SSH server
			c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				Timeout:         5 * time.Second,
			})
			if err != nil {
				return fmt.Errorf("failed to connect to ssh: %w", err)
			}
			defer c.Close()

			// 3. Start an ssh client which requests a local port forward
			localListener, localAddr, err := getRandomListener()
			if err != nil {
				return fmt.Errorf("failed to create local listener: %w", err)
			}
			localListener.Close() // Close it so we can use the port

			// Extract local port from address
			_, localPort, err := net.SplitHostPort(localAddr)
			if err != nil {
				return fmt.Errorf("failed to parse local port: %w", err)
			}

			// Request port forwarding from local port to http server port
			localConn, err := c.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", localPort))
			if err != nil {
				return fmt.Errorf("failed to setup port forwarding: %w", err)
			}
			defer localConn.Close()

			// Handle forwarding connections in background
			go func() {
				for {
					conn, err := localConn.Accept()
					if err != nil {
						return
					}
					go func(conn net.Conn) {
						defer conn.Close()
						// Forward to HTTP server
						httpConn, err := net.Dial("tcp", httpAddr)
						if err != nil {
							return
						}
						defer httpConn.Close()

						// Bidirectional copy
						go io.Copy(httpConn, conn)
						io.Copy(conn, httpConn)
					}(conn)
				}
			}()

			// 4. HTTP client GET the local port forward / and expect "foo"
			httpClient := &http.Client{Timeout: 3 * time.Second}
			resp, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%s/", localPort))
			if err != nil {
				return fmt.Errorf("failed to make http request through port forward: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read response body: %w", err)
			}

			if string(body) != "foo" {
				return fmt.Errorf("unexpected response: got %q, want %q", string(body), "foo")
			}

			return nil
		},
	},
}

// newTestServer creates a new server for testing
func newTestServer(ctx context.Context, c *Config) (string, <-chan error) {
	// 5 second max time for tests
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	done := make(chan error)
	errf := func(format string, args ...interface{}) {
		done <- fmt.Errorf(format, args...)
		cancel()
	}
	// default server config
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.AuthType == "" {
		c.AuthType = "none"
	}
	if c.KeySeed == "" && c.KeyFile == "" {
		c.KeySeed = "test-key-seed-12345"
	}
	server, err := NewServer(c)
	if err != nil {
		go errf("Failed to create server: %v", err)
		return "", done
	}
	// Listen on a random port
	listener, _, err := getRandomListener()
	if err != nil {
		// t.Fatalf("Failed to create listener: %v", err)
		go errf("Failed to create listener: %v", err)
		return "", done
	}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	// Get the actual port
	addr := listener.Addr().(*net.TCPAddr)
	// Start server in background
	go func() {
		err := server.startWith(listener)
		if err != nil {
			errf("Server failed: %v", err)
		}
	}()
	return addr.String(), nil
}

func TestAll(t *testing.T) {
	t.Parallel()
	for i, tc := range testCases {
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
