//go:build windows

package sshd_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
	"github.com/jpillora/sshd-lite/sshd/xnet"
	"golang.org/x/crypto/ssh"
)

// testServer is a running sshd-lite plus the client key needed to reach it.
type testServer struct {
	port    string
	keyFile string
	done    <-chan error
}

// startTestServer boots a server using shell for sessions. An empty shell
// leaves Config.Shell unset, which selects the platform default.
func startTestServer(t *testing.T, shell string) *testServer {
	t.Helper()

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "test_key")

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "user")
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		t.Fatalf("Failed to write private key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(privKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	portNum, err := xnet.FindFreePort()
	if err != nil {
		t.Fatalf("Failed to get random port: %v", err)
	}
	port := fmt.Sprintf("%d", portNum)

	c := sshd.Config{
		Host:       "127.0.0.1",
		Port:       port,
		Shell:      shell,
		AuthKeys:   []ssh.PublicKey{sshPubKey},
		KeySeed:    "test-key-seed-12345",
		LogVerbose: true,
	}

	server, err := sshd.NewServer(c)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		if err := server.StartContext(ctx); err != nil {
			serverDone <- err
		}
	}()

	time.Sleep(200 * time.Millisecond)
	t.Cleanup(func() {
		cancel()
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			t.Log("Server did not stop within 5 seconds after cancel")
		}
	})

	return &testServer{port: port, keyFile: keyFile, done: serverDone}
}

// run executes command over SSH and returns its combined output and the exit
// status the client reported.
func (s *testServer) run(t *testing.T, command string) (string, int) {
	t.Helper()

	// A hang here is the failure mode this suite exists to catch: handing
	// cmd.exe a flag it does not understand leaves it waiting on input that
	// never arrives, so the session stalls rather than erroring.
	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cmdCancel()

	sshArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=NUL",
		"-i", s.keyFile,
		"user@127.0.0.1",
		"-p", s.port,
		command,
	}
	sshCmd := osexec.CommandContext(cmdCtx, "ssh", sshArgs...)
	out, err := sshCmd.CombinedOutput()
	t.Logf("SSH output: %s", string(out))

	exitCode := 0
	if err != nil {
		t.Logf("SSH command exited with error: %v", err)
		var exitErr *osexec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	if cmdCtx.Err() == context.DeadlineExceeded {
		t.Fatalf("SSH command timed out after 30s — the session hung instead of running %q", command)
	}
	return string(out), exitCode
}

func TestWindowsPowerShellCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Windows integration test in short mode")
	}

	server := startTestServer(t, "")
	out, _ := server.run(t, "echo hello-windows; exit 0")

	if !strings.Contains(out, "hello-windows") {
		t.Errorf("Expected 'hello-windows' in output, got: %s", out)
	}

	time.Sleep(3 * time.Second)

	select {
	case err := <-server.done:
		t.Errorf("Server exited after client disconnect - this is a bug! Error: %v", err)
	default:
		t.Log("Server survived client disconnect - test passed")
	}
}

// TestWindowsCmdCommand covers remote execution with cmd.exe, which needs /c
// where PowerShell and POSIX shells take -c. This previously hung until the
// client gave up, because cmd.exe does not reject the unknown flag.
func TestWindowsCmdCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Windows integration test in short mode")
	}

	server := startTestServer(t, "cmd.exe")
	out, _ := server.run(t, "echo hello-from-cmd")

	if !strings.Contains(out, "hello-from-cmd") {
		t.Errorf("Expected 'hello-from-cmd' in output, got: %s", out)
	}
}

// TestWindowsCmdCommandExitStatus checks that a cmd.exe failure still reports a
// non-zero exit status rather than being swallowed by the shell invocation.
func TestWindowsCmdCommandExitStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Windows integration test in short mode")
	}

	server := startTestServer(t, "cmd.exe")
	_, exitCode := server.run(t, "exit 3")

	if exitCode != 3 {
		t.Errorf("Expected exit status 3 from cmd.exe, got %d", exitCode)
	}
}
