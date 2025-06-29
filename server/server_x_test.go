package sshd_test

// test helpers

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	sshd "github.com/jpillora/sshd-lite/server"
	"golang.org/x/crypto/ssh"
)

// newTestServer creates a new server for testing
func newTestServer(ctx context.Context, c *sshd.Config) (string, <-chan error) {
	// 5 second max time for tests
	_, cancel := context.WithTimeout(ctx, 5*time.Second)
	done := make(chan error)
	errf := func(format string, args ...interface{}) {
		done <- fmt.Errorf(format, args...)
		cancel()
	}

	// Create a random port
	port, err := getRandomPort()
	if err != nil {
		go errf("Failed to get random port: %v", err)
		return "", done
	}

	// default server config
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	c.Port = port // Use the random port we found
	if c.AuthType == "" {
		c.AuthType = "none"
	}
	if c.KeySeed == "" && c.KeyFile == "" {
		c.KeySeed = "test-key-seed-12345"
	}

	server, err := sshd.NewServer(c)
	if err != nil {
		go errf("Failed to create server: %v", err)
		return "", done
	}

	// Start server in background
	go func() {
		err := server.Start()
		if err != nil {
			errf("Server failed: %v", err)
		}
	}()

	// Wait a bit for the server to start
	time.Sleep(100 * time.Millisecond)

	return fmt.Sprintf("127.0.0.1:%s", port), done
}

// getRandomListener creates a listener on a random port and returns it along with the address
func getRandomListener() (net.Listener, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	addr := listener.Addr().(*net.TCPAddr)
	return listener, addr.String(), nil
}

// getRandomPort returns a random available port number
func getRandomPort() (string, error) {
	listener, addr, err := getRandomListener()
	if err != nil {
		return "", err
	}
	listener.Close()

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}

	return port, nil
}

// createSSHClient creates an SSH client connection to the given address
func createSSHClient(addr string) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}

// createTestHTTPServer creates a test HTTP server that responds with the given message
func createTestHTTPServer(message string) (net.Listener, *http.Server, string, error) {
	listener, addr, err := getRandomListener()
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create listener: %w", err)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Write([]byte(message))
			} else {
				http.NotFound(w, r)
			}
		}),
	}

	go server.Serve(listener)
	return listener, server, addr, nil
}

// testHTTPGet performs an HTTP GET request and validates the response
func testHTTPGet(url, expectedResponse string) error {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to make http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if string(body) != expectedResponse {
		return fmt.Errorf("unexpected response: got %q, want %q", string(body), expectedResponse)
	}

	return nil
}

// forwardConnections handles bidirectional forwarding between two connections
func forwardConnections(conn1, conn2 net.Conn) {
	defer conn1.Close()
	defer conn2.Close()

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(conn1, conn2)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(conn2, conn1)
		done <- struct{}{}
	}()

	<-done // Wait for at least one direction to complete
}
