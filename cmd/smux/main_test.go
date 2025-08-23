package main

import (
	"os"
	"testing"

	"github.com/jpillora/sshd-lite/server"
)

func TestDaemonProcessManagement(t *testing.T) {
	// Use temporary paths for testing
	testPIDPath := "/tmp/smux_test.pid"
	testSocketPath := "/tmp/smux_test.sock"
	
	// Clean up before test
	os.Remove(testPIDPath)
	os.Remove(testSocketPath)
	
	// Test PID file handling
	testPID := "12345"
	err := os.WriteFile(testPIDPath, []byte(testPID), 0644)
	if err != nil {
		t.Fatalf("Failed to write test PID file: %v", err)
	}
	
	// Clean up
	os.Remove(testPIDPath)
}

func TestSessionManager(t *testing.T) {
	// Test session manager
	sm := sshd.NewSessionManager()
	
	// Test empty sessions
	sessions := sm.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("Expected 0 sessions, got %d", len(sessions))
	}
	
	// Add a session
	sm.AddSession("test-id", "bash", 1234)
	sessions = sm.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}
	
	session := sessions[0]
	if session.ID != "test-id" || session.Name != "bash" || session.PID != 1234 {
		t.Fatalf("Session data mismatch: %+v", session)
	}
	
	// Test JSON serialization
	data, err := sm.GetSessionsJSON()
	if err != nil {
		t.Fatalf("Failed to get sessions JSON: %v", err)
	}
	
	if len(data) == 0 {
		t.Fatal("Expected non-empty JSON data")
	}
	
	// Remove session
	sm.RemoveSession("test-id")
	sessions = sm.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("Expected 0 sessions after removal, got %d", len(sessions))
	}
}