//go:build windows
// +build windows

package sshd_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
)

func TestWindowsPowerShellCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Windows integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "sshd-lite-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	keyFile := filepath.Join(tmpDir, "test_key")

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "user")
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(keyFile, pemBytes, 0600); err != nil {
		t.Fatalf("Failed to write private key: %v", err)
	}

	pubEd25519 := privKey.Public().(ed25519.PublicKey)
	sshPubKey, err := ssh.NewPublicKey(pubEd25519)
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	c := &sshd.Config{
		Host:       "127.0.0.1",
		AuthKeys:   []ssh.PublicKey{sshPubKey},
		KeySeed:    "test-key-seed-12345",
		LogVerbose: true,
	}

	port, err := getRandomPort()
	if err != nil {
		t.Fatalf("Failed to get random port: %v", err)
	}
	c.Port = port

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
	defer cancel()
	defer func() {
		cancel()
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			t.Log("Server did not stop within 5 seconds after cancel")
		}
	}()

	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cmdCancel()

	sshArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=NUL",
		"-i", keyFile,
		"user@127.0.0.1",
		"-p", port,
		"echo hello-windows; exit 0",
	}
	sshCmd := osexec.CommandContext(cmdCtx, "ssh", sshArgs...)
	out, err := sshCmd.CombinedOutput()
	t.Logf("SSH output: %s", string(out))

	if err != nil {
		t.Logf("SSH command exited with error (expected): %v", err)
	}

	if !strings.Contains(string(out), "hello-windows") {
		t.Errorf("Expected 'hello-windows' in output, got: %s", string(out))
	}

	time.Sleep(3 * time.Second)

	select {
	case err := <-serverDone:
		t.Errorf("Server exited after client disconnect - this is a bug! Error: %v", err)
	default:
		t.Log("Server survived client disconnect - test passed")
	}
}
