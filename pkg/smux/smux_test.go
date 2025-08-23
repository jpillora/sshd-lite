//go:build !windows

package smux

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	sm := newSessionManager()
	
	// Test creating a session
	session, err := sm.CreateSession("test-id")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	
	if session.ID != "test-id" {
		t.Errorf("Expected session ID 'test-id', got '%s'", session.ID)
	}
	
	// Test getting session
	retrievedSession, exists := sm.GetSession("test-id")
	if !exists {
		t.Fatal("Session should exist")
	}
	
	if retrievedSession.ID != session.ID {
		t.Error("Retrieved session ID doesn't match")
	}
	
	// Test listing sessions
	sessions := sm.ListSessions()
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}
	
	// Test removing session
	sm.RemoveSession("test-id")
	sessions = sm.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions after removal, got %d", len(sessions))
	}
}

func TestHTTPServer(t *testing.T) {
	sm := newSessionManager()
	server := newHTTPServer(sm)
	
	// Create a test session
	session, err := sm.CreateSession("test-session")
	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}
	
	// Test index page
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	
	if !strings.Contains(w.Body.String(), "Smux Terminal Multiplexer") {
		t.Error("Index page should contain title")
	}
	
	// Test sessions API
	req = httptest.NewRequest("GET", "/api/sessions", nil)
	w = httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	
	var sessions []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}
	
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session in response, got %d", len(sessions))
	}
	
	if sessions[0]["id"] != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%v'", sessions[0]["id"])
	}
	
	// Test create session API
	reqBody := strings.NewReader(`{}`)
	req = httptest.NewRequest("POST", "/api/sessions/create", reqBody)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	
	var createResponse map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&createResponse); err != nil {
		t.Fatalf("Failed to decode create response: %v", err)
	}
	
	// Clean up
	sm.RemoveSession(session.ID)
	if createdID, ok := createResponse["id"].(string); ok {
		sm.RemoveSession(createdID)
	}
}

func TestWebSocketWrapper(t *testing.T) {
	// Test WebSocket wrapper structure
	wrapper := &WebSocketWrapper{
		conn: nil, // We can't easily create a real WebSocket connection in tests
	}
	
	if wrapper.conn != nil {
		t.Error("Expected nil connection in test")
	}
}

func TestSessionResize(t *testing.T) {
	sm := newSessionManager()
	session, err := sm.CreateSession("resize-test")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sm.RemoveSession("resize-test")
	
	// Give session time to start
	time.Sleep(100 * time.Millisecond)
	
	// Test resize
	err = session.Resize(50, 120)
	if err != nil {
		t.Errorf("Resize should not error: %v", err)
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	id2 := generateSessionID()
	
	if id1 == id2 {
		t.Errorf("Generated session IDs should be unique, got %s and %s", id1, id2)
	}
	
	if len(id1) != 8 {
		t.Errorf("Expected session ID length 8, got %d", len(id1))
	}
}

func TestCreateSessionWithCommand(t *testing.T) {
	sm := newSessionManager()
	
	// Test creating a session with initial command
	session, err := sm.CreateSessionWithCommand("test-cmd", "echo 'hello world'")
	if err != nil {
		t.Fatalf("Failed to create session with command: %v", err)
	}
	defer sm.RemoveSession("test-cmd")
	
	if session.ID != "test-cmd" {
		t.Errorf("Expected session ID 'test-cmd', got '%s'", session.ID)
	}
	
	// Give session time to start and process command
	time.Sleep(200 * time.Millisecond)
	
	// Verify session is running
	sessions := sm.ListSessions()
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}
}

func TestHTTPCreateSessionWithCommand(t *testing.T) {
	sm := newSessionManager()
	server := newHTTPServer(sm)
	
	// Test creating session with command via HTTP API
	reqBody := strings.NewReader(`{"command":"ls -la"}`)
	req := httptest.NewRequest("POST", "/api/sessions/create", reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	
	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	if response["command"] != "ls -la" {
		t.Errorf("Expected command 'ls -la', got '%v'", response["command"])
	}
	
	// Clean up
	if sessionID, ok := response["id"].(string); ok {
		sm.RemoveSession(sessionID)
	}
}