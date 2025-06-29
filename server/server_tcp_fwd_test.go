package sshd_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	sshd "github.com/jpillora/sshd-lite/server"
)

var tcpForwardingLocal = testCase{
	name: "tcp-forwarding-local",
	server: &sshd.Config{
		TCPForwarding: true,
		LogVerbose:    true,
	},
	client: func(addr string) error {
		// 1. Start a test HTTP server
		httpListener, httpServer, httpAddr, err := createTestHTTPServer("foo")
		if err != nil {
			return err
		}
		defer httpListener.Close()
		defer httpServer.Close()

		// 2. Connect to SSH server
		c, err := createSSHClient(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to ssh: %w", err)
		}
		defer c.Close()

		// 3. Setup local port forwarding (reverse tunnel)
		// This is actually "remote" forwarding from the SSH server's perspective:
		// - We ask the SSH server to listen on a port
		// - When connections come in, the server forwards them back to us
		// - We then forward them to our local HTTP server
		localPort, err := getRandomPort()
		if err != nil {
			return fmt.Errorf("failed to get random port: %w", err)
		}

		// Request the SSH server to listen on this port and forward connections back to us
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
					httpConn, err := net.Dial("tcp", httpAddr)
					if err != nil {
						conn.Close()
						return
					}
					forwardConnections(conn, httpConn)
				}(conn)
			}
		}()

		// 4. Test HTTP request through port forward
		return testHTTPGet(fmt.Sprintf("http://127.0.0.1:%s/", localPort), "foo")
	},
}

var tcpForwardingRemote = testCase{
	name: "tcp-forwarding-remote",
	server: &sshd.Config{
		TCPForwarding: true,
		LogVerbose:    true,
	},
	client: func(addr string) error {
		// 1. Connect to SSH server
		c, err := createSSHClient(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to ssh: %w", err)
		}
		defer c.Close()

		// 2. Request remote port forwarding (reverse tunnel)
		// This asks the SSH server to listen on a port and forward incoming connections back to us
		// c.Listen() creates a "reverse tunnel" - the server listens, we handle the connections
		remoteListener, err := c.Listen("tcp", "127.0.0.1:0") // 0 = random port
		if err != nil {
			return fmt.Errorf("failed to setup remote port forwarding: %w", err)
		}
		defer remoteListener.Close()

		// Get the actual remote port that was allocated by the SSH server
		remoteAddr := remoteListener.Addr().String()

		// 3. Start our local HTTP server that will receive the forwarded connections
		httpListener, httpServer, httpAddr, err := createTestHTTPServer("bar")
		if err != nil {
			return err
		}
		defer httpListener.Close()
		defer httpServer.Close()

		// 4. Handle incoming connections from the remote port forwarding
		// When someone connects to the SSH server's bound port, we'll get the connection here
		connectionReceived := make(chan error, 1)
		go func() {
			conn, err := remoteListener.Accept()
			if err != nil {
				connectionReceived <- fmt.Errorf("failed to accept remote connection: %w", err)
				return
			}

			// Forward this connection to our local HTTP server
			httpConn, err := net.Dial("tcp", httpAddr)
			if err != nil {
				conn.Close()
				connectionReceived <- fmt.Errorf("failed to connect to local http server: %w", err)
				return
			}

			connectionReceived <- nil
			forwardConnections(conn, httpConn)
		}()

		// 5. Make an HTTP request to the remote port
		// This simulates an external client connecting to the SSH server's bound port
		// The SSH server will forward this connection back to us through the tunnel
		httpClient := &http.Client{Timeout: 3 * time.Second}
		resp, err := httpClient.Get(fmt.Sprintf("http://%s/", remoteAddr))
		if err != nil {
			return fmt.Errorf("failed to make http request to remote port: %w", err)
		}
		defer resp.Body.Close()

		// Wait for the connection to be established
		select {
		case err := <-connectionReceived:
			if err != nil {
				return err
			}
		case <-time.After(2 * time.Second):
			return fmt.Errorf("timeout waiting for remote connection")
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		if string(body) != "bar" {
			return fmt.Errorf("unexpected response: got %q, want %q", string(body), "bar")
		}

		return nil
	},
}
