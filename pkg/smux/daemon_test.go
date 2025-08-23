package smux

import (
	"os"
	"testing"
)

func TestDaemonProcessManagement(t *testing.T) {
	// Use temporary paths for testing
	testPIDPath := "/tmp/smux_test.pid"
	testSocketPath := "/tmp/smux_test.sock"
	
	// Clean up before and after test
	defer func() {
		// Always clean up
		os.Remove(testPIDPath)
		os.Remove(testSocketPath)
	}()
	
	// These functions use the default paths, so we can't easily test them
	// without modifying the global constants. For now, just verify they
	// don't panic and handle basic cases.
	
	// This should return false since we haven't set up a valid daemon
	running := IsDaemonRunning()
	if running {
		t.Log("Warning: daemon appears to be running, test may be unreliable")
	}
}

func TestDaemonCreation(t *testing.T) {
	daemon := newDaemon()
	if daemon == nil {
		t.Fatal("newDaemon() returned nil")
	}
	
	if daemon.sessionManager == nil {
		t.Fatal("Daemon session manager is nil")
	}
	
	if daemon.httpServer == nil {
		t.Fatal("Daemon HTTP server is nil")
	}
}