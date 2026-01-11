package xhttp

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/jpillora/sshd-lite/sshd/xnet"
)

// TestServer represents a test HTTP server.
type TestServer struct {
	Listener net.Listener
	Server   *http.Server
	Addr     string
}

// Close closes the HTTP test server.
func (s *TestServer) Close() {
	s.Listener.Close()
	s.Server.Close()
}

// NewTestServer creates a test HTTP server that responds with the given message.
func NewTestServer(message string) (*TestServer, error) {
	listener, addr, err := xnet.GetRandomListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				if _, err := w.Write([]byte(message)); err != nil {
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			} else {
				http.NotFound(w, r)
			}
		}),
	}

	go func() {
		server.Serve(listener) //nolint:errcheck
	}()

	return &TestServer{
		Listener: listener,
		Server:   server,
		Addr:     addr,
	}, nil
}

// TestGet performs an HTTP GET request and validates the response.
func TestGet(url, expectedResponse string) error {
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
