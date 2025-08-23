//go:build windows

package smux

import (
	"testing"
)

// On Windows, PTY operations may not be fully supported, so we provide
// minimal tests to ensure the package builds and basic functionality works.

func TestDaemonProcessManagement(t *testing.T) {
	// This test doesn't use PTY, so it should work on Windows
	if IsDaemonRunning() {
		t.Log("Warning: daemon appears to be running, test may be unreliable")
	}
}

func TestDaemonCreation(t *testing.T) {
	// This test doesn't use PTY, so it should work on Windows
	daemon := &daemon{
		sessionManager: newSessionManager(),
	}
	if daemon.sessionManager == nil {
		t.Error("Failed to create session manager")
	}
}

func TestWebSocketWrapper(t *testing.T) {
	// Test WebSocket wrapper structure without PTY
	wrapper := &WebSocketWrapper{
		conn: nil,
	}
	
	if wrapper.conn != nil {
		t.Error("Expected nil connection in test")
	}
}

func TestGenerateSessionID(t *testing.T) {
	// This doesn't require PTY
	id1 := generateSessionID()
	id2 := generateSessionID()
	
	if id1 == id2 {
		t.Errorf("Generated session IDs should be unique, got %s and %s", id1, id2)
	}
	
	if len(id1) != 8 {
		t.Errorf("Expected session ID length 8, got %d", len(id1))
	}
}