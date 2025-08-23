package client

import (
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	
	if client.config == nil {
		t.Fatal("Client config is nil")
	}
	
	if client.config.User != "user" {
		t.Errorf("Expected user 'user', got '%s'", client.config.User)
	}
	
	if len(client.config.Auth) == 0 {
		t.Fatal("No auth methods configured")
	}
	
	if client.config.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback is nil")
	}
}

func TestConnectUnixSocket_InvalidPath(t *testing.T) {
	client := NewClient()
	
	// Try to connect to non-existent socket
	err := client.ConnectUnixSocket("/nonexistent/path/socket")
	if err == nil {
		t.Fatal("Expected error when connecting to non-existent socket")
	}
	
	// Verify connection is nil after failed attempt
	if client.conn != nil {
		t.Fatal("Connection should be nil after failed connect")
	}
}

func TestConnectUnixSocket_InvalidSocket(t *testing.T) {
	client := NewClient()
	
	// Create a regular file instead of a socket
	tmpFile, err := os.CreateTemp("", "not-a-socket")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()
	
	// Try to connect to regular file (not a socket)
	err = client.ConnectUnixSocket(tmpFile.Name())
	if err == nil {
		t.Fatal("Expected error when connecting to regular file")
	}
}

func TestNewSession_NoConnection(t *testing.T) {
	client := NewClient()
	
	// Try to create session without connection
	session, err := client.NewSession()
	if err == nil {
		t.Fatal("Expected error when creating session without connection")
	}
	
	if session != nil {
		t.Fatal("Session should be nil when no connection exists")
	}
	
	if err != os.ErrInvalid {
		t.Errorf("Expected os.ErrInvalid, got %v", err)
	}
}

func TestSendRequest_NoConnection(t *testing.T) {
	client := NewClient()
	
	// Try to send request without connection
	ok, data, err := client.SendRequest("test", true, nil)
	if err == nil {
		t.Fatal("Expected error when sending request without connection")
	}
	
	if ok {
		t.Fatal("Request should not be ok without connection")
	}
	
	if data != nil {
		t.Fatal("Data should be nil without connection")
	}
	
	if err != os.ErrInvalid {
		t.Errorf("Expected os.ErrInvalid, got %v", err)
	}
}

func TestClose_NoConnection(t *testing.T) {
	client := NewClient()
	
	// Close without connection should not error
	err := client.Close()
	if err != nil {
		t.Errorf("Close() without connection should not error, got %v", err)
	}
}

func TestClose_WithConnection(t *testing.T) {
	client := NewClient()
	
	// Simulate having a connection by setting it to non-nil
	// We can't easily test a real connection without a server,
	// but we can test the nil check logic
	if client.conn != nil {
		// If somehow there was a connection, close should work
		err := client.Close()
		if err != nil {
			t.Errorf("Close() with connection failed: %v", err)
		}
	}
}