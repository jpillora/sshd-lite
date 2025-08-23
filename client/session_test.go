package client

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestTerminalSessionInterface(t *testing.T) {
	// Test that our session types implement the interface
	var _ TerminalSession = &SSHSession{}
	var _ TerminalSession = &PTYSession{}
	var _ TerminalSession = &WebSocketSession{}
}

func TestPTYSession(t *testing.T) {
	// We can't easily test PTY sessions without actually creating PTYs
	// But we can test the structure
	ptySession := NewPTYSession(nil)
	if ptySession == nil {
		t.Fatal("NewPTYSession returned nil")
	}
	
	// Test that methods don't panic with nil PTY
	err := ptySession.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	if err == nil {
		t.Error("Expected error with nil PTY")
	}
	
	err = ptySession.Shell()
	if err != nil {
		t.Errorf("Shell should not error: %v", err)
	}
}

func TestWebSocketSession(t *testing.T) {
	// Test WebSocket session wrapper
	ptySession := NewPTYSession(nil)
	reader := strings.NewReader("test input")
	writer := &bytes.Buffer{}
	
	wsSession := NewWebSocketSession(ptySession, reader, writer)
	if wsSession == nil {
		t.Fatal("NewWebSocketSession returned nil")
	}
	
	if wsSession.ptySession != ptySession {
		t.Error("PTY session not properly set")
	}
	
	if wsSession.wsReader != reader {
		t.Error("WebSocket reader not properly set")
	}
	
	if wsSession.wsWriter != writer {
		t.Error("WebSocket writer not properly set")
	}
}

func TestAttachWebSocketToSession(t *testing.T) {
	// Test the WebSocket attachment function
	ptySession := NewPTYSession(nil)
	reader := strings.NewReader("test")
	writer := &bytes.Buffer{}
	
	wsSession := AttachWebSocketToSession(ptySession, reader, writer)
	if wsSession == nil {
		t.Fatal("AttachWebSocketToSession returned nil")
	}
	
	// Verify it's a WebSocket session
	if wsSession.ptySession != ptySession {
		t.Error("PTY session not properly attached")
	}
}

func TestSSHSessionWithMock(t *testing.T) {
	// Test with the existing mock session from terminal_test.go
	mockSession := &mockSession{}
	
	// Test that we can create an SSH session wrapper
	// We'll test the interface compliance rather than the specific implementation
	mockSession.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	if !mockSession.ptyRequested {
		t.Error("Mock session should track PTY requests")
	}
	
	mockSession.Shell()
	if !mockSession.shellRequested {
		t.Error("Mock session should track shell requests")
	}
	
	mockSession.WindowChange(25, 100)
	if len(mockSession.windowChanges) != 1 {
		t.Error("Mock session should track window changes")
	}
	
	mockSession.Close()
	if !mockSession.closed {
		t.Error("Mock session should track close calls")
	}
}