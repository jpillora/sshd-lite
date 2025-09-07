//go:build !windows

package smux

import (
	"os"
	"testing"
)

func TestDaemonProcessManagement(t *testing.T) {
	testPIDPath := "/tmp/smux_test.pid"
	testSocketPath := "/tmp/smux_test.sock"
	
	defer func() {
		os.Remove(testPIDPath)
		os.Remove(testSocketPath)
	}()
	
	config := Config{
		PIDPath:    testPIDPath,
		SocketPath: testSocketPath,
		HTTPPort:   HTTPPort,
	}
	daemon := NewDaemon(config)
	
	running := daemon.IsRunning()
	if running {
		t.Log("Warning: daemon appears to be running, test may be unreliable")
	}
}

func TestDaemonCreation(t *testing.T) {
	config := Config{HTTPPort: HTTPPort}
	daemon := NewDaemon(config)
	if daemon == nil {
		t.Fatal("NewDaemon() returned nil")
	}
	
	if daemon.sessionManager == nil {
		t.Fatal("Daemon session manager is nil")
	}
	
	if daemon.httpServer == nil {
		t.Fatal("Daemon HTTP server is nil")
	}
}