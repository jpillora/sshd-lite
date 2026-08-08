package sshd

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sshdkey "github.com/jpillora/sshd-lite/sshd/key"
	"golang.org/x/crypto/ssh"
)

func TestFileAuthInitialLoadFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, path string)
		wantCause string
	}{
		{
			name:      "missing",
			prepare:   func(t *testing.T, path string) {},
			wantCause: "read authorized keys file",
		},
		{
			name: "unreadable",
			prepare: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantCause: "read authorized keys file",
		},
		{
			name: "empty",
			prepare: func(t *testing.T, path string) {
				writeAuthFile(t, path, nil)
			},
			wantCause: "parse authorized keys file",
		},
		{
			name: "malformed",
			prepare: func(t *testing.T, path string) {
				writeAuthFile(t, path, []byte("not an authorized key\n"))
			},
			wantCause: "parse authorized keys file",
		},
		{
			name: "oversized",
			prepare: func(t *testing.T, path string) {
				writeAuthFile(t, path, bytes.Repeat([]byte("x"), maxAuthorizedKeysFileSize+1))
			},
			wantCause: "exceeds 1048576-byte limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "authorized_keys")
			tt.prepare(t, path)

			_, err := NewServer(fileAuthConfig(path, nil))
			if err == nil {
				t.Fatal("NewServer succeeded with an invalid authorized keys file")
			}
			if !strings.Contains(err.Error(), "initialize authorized keys") ||
				!strings.Contains(err.Error(), tt.wantCause) ||
				!strings.Contains(err.Error(), path) {
				t.Fatalf("NewServer error %q does not contain the expected context", err)
			}
		})
	}
}

func TestFileAuthReloadFailsClosedAndRecovers(t *testing.T) {
	keyA := publicKeyFromSeed(t, "file-auth-a")
	keyB := publicKeyFromSeed(t, "file-auth-b")
	entryA := ssh.MarshalAuthorizedKey(keyA)
	entryB := ssh.MarshalAuthorizedKey(keyB)
	path := filepath.Join(t.TempDir(), "authorized_keys")
	writeAuthFile(t, path, entryA)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	server, err := NewServer(fileAuthConfig(path, logger))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	callback := server.sshConfig.PublicKeyCallback

	// An unchanged valid file remains usable.
	requireFileAuth(t, callback, keyA, true)
	requireFileAuth(t, callback, keyA, true)

	// A valid replacement both authorizes the new key and revokes the old one.
	writeAuthFile(t, path, entryB)
	requireFileAuth(t, callback, keyA, false)
	requireFileAuth(t, callback, keyB, true)

	// Every invalid post-startup state denies access instead of retaining key B.
	writeAuthFile(t, path, nil)
	requireFileAuth(t, callback, keyB, false)
	requireFileAuth(t, callback, keyB, false)
	writeAuthFile(t, path, []byte("malformed authorized key data\n"))
	requireFileAuth(t, callback, keyB, false)
	writeAuthFile(t, path, bytes.Repeat([]byte("x"), maxAuthorizedKeysFileSize+1))
	requireFileAuth(t, callback, keyB, false)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	requireFileAuth(t, callback, keyB, false)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	requireFileAuth(t, callback, keyB, false)

	// Recreating a valid file recovers without rebuilding the server.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeAuthFile(t, path, entryA)
	requireFileAuth(t, callback, keyA, true)
	requireFileAuth(t, callback, keyB, false)

	logOutput := logs.String()
	if !strings.Contains(logOutput, "Failed to reload authorized keys") ||
		!strings.Contains(logOutput, path) {
		t.Fatalf("reload failure log lacks useful context: %s", logOutput)
	}
	if strings.Contains(logOutput, "malformed authorized key data") {
		t.Fatalf("reload failure log contains file contents: %s", logOutput)
	}
	if !strings.Contains(logOutput, "exceeds 1048576-byte limit") {
		t.Fatalf("oversized-file log lacks limit context: %s", logOutput)
	}
	if count := strings.Count(logOutput, "parse authorized keys file"); count != 1 {
		t.Fatalf("identical consecutive parse failures logged %d times, want 1: %s", count, logOutput)
	}
}

func TestFileAuthConcurrentReload(t *testing.T) {
	keyA := publicKeyFromSeed(t, "file-auth-concurrent-a")
	keyB := publicKeyFromSeed(t, "file-auth-concurrent-b")
	path := filepath.Join(t.TempDir(), "authorized_keys")
	writeAuthFile(t, path, ssh.MarshalAuthorizedKey(keyA))

	server, err := NewServer(fileAuthConfig(path, nil))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	callback := server.sshConfig.PublicKeyCallback

	// Give every callback a changed file to observe. Concurrent callbacks raced
	// while updating the old closure's cached timestamp and map.
	writeAuthFile(t, path, ssh.MarshalAuthorizedKey(keyB))
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("set authorized keys timestamp: %v", err)
	}

	const goroutines = 64
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := callback(nil, keyB)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent authentication failed: %v", err)
		}
	}

	// Concurrent failures also exercise the synchronized log-deduplication
	// state while every callback independently fails closed.
	writeAuthFile(t, path, nil)
	start = make(chan struct{})
	errs = make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := callback(nil, keyB)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil || err.Error() != "denied" {
			t.Errorf("concurrent invalid-file authentication returned %v, want denied", err)
		}
	}
}

func TestFileAuthRechecksAfterSignatureVerification(t *testing.T) {
	signer, err := sshdkey.SignerFromSeed("file-auth-signed-request")
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}
	path := filepath.Join(t.TempDir(), "authorized_keys")
	entry := ssh.MarshalAuthorizedKey(signer.PublicKey())
	writeAuthFile(t, path, entry)

	server, err := NewServer(fileAuthConfig(path, nil))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Pause the first successful unsigned key query before the server replies.
	// The standard SSH client signs only after receiving that reply, making the
	// subsequent file change deterministic rather than timing-dependent.
	queryApproved := make(chan struct{})
	resumeQuery := make(chan struct{})
	publicKeyCallback := server.sshConfig.PublicKeyCallback
	var queryOnce sync.Once
	server.sshConfig.PublicKeyCallback = func(conn ssh.ConnMetadata, publicKey ssh.PublicKey) (*ssh.Permissions, error) {
		permissions, err := publicKeyCallback(conn, publicKey)
		if err == nil {
			queryOnce.Do(func() {
				close(queryApproved)
				<-resumeQuery
			})
		}
		return permissions, err
	}

	verified := make(chan struct{})
	verifiedPublicKeyCallback := server.sshConfig.VerifiedPublicKeyCallback
	var verifiedOnce sync.Once
	server.sshConfig.VerifiedPublicKeyCallback = func(conn ssh.ConnMetadata, publicKey ssh.PublicKey, permissions *ssh.Permissions, signatureAlgorithm string) (*ssh.Permissions, error) {
		verifiedOnce.Do(func() { close(verified) })
		return verifiedPublicKeyCallback(conn, publicKey, permissions, signatureAlgorithm)
	}

	result := startSSHHandshake(t, server.sshConfig, signer)
	select {
	case <-queryApproved:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for public-key query approval")
	}
	writeAuthFile(t, path, nil)
	close(resumeQuery)

	revoked := waitSSHHandshake(t, result)
	if revoked.clientErr == nil {
		t.Fatal("signed authentication succeeded with a key revoked after its query")
	}
	if strings.Contains(revoked.clientErr.Error(), path) || strings.Contains(revoked.clientErr.Error(), "authorized keys") {
		t.Fatalf("client error exposed authorized-key reload details: %v", revoked.clientErr)
	}
	select {
	case <-verified:
	default:
		t.Fatal("signed authentication did not reach VerifiedPublicKeyCallback")
	}

	// Repairing the file recovers the same server, and leaving it unchanged
	// continues to authenticate on a later independent handshake.
	writeAuthFile(t, path, entry)
	recovered := waitSSHHandshake(t, startSSHHandshake(t, server.sshConfig, signer))
	requireSSHHandshakeSuccess(t, recovered)
	unchanged := waitSSHHandshake(t, startSSHHandshake(t, server.sshConfig, signer))
	requireSSHHandshakeSuccess(t, unchanged)
}

func fileAuthConfig(path string, logger *slog.Logger) Config {
	return Config{
		AuthType:  path,
		KeySeed:   "server-config-test",
		KeySeedEC: true,
		LogQuiet:  logger == nil,
		Logger:    logger,
	}
}

func publicKeyFromSeed(t *testing.T, seed string) ssh.PublicKey {
	t.Helper()
	publicKey, err := sshdkey.PublicKeyFromSeed(seed)
	if err != nil {
		t.Fatalf("generate public key: %v", err)
	}
	return publicKey
}

func writeAuthFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write authorized keys: %v", err)
	}
}

func requireFileAuth(t *testing.T, callback func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error), publicKey ssh.PublicKey, wantAllowed bool) {
	t.Helper()
	_, err := callback(nil, publicKey)
	if wantAllowed {
		if err != nil {
			t.Fatalf("expected key to be authorized, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("expected key to be denied")
	}
	if got := err.Error(); got != "denied" {
		t.Fatalf("authentication exposed reload details to client: %q", got)
	}
}

type sshHandshakeResult struct {
	clientErr error
	serverErr error
}

func startSSHHandshake(t *testing.T, config *ssh.ServerConfig, signer ssh.Signer) <-chan sshHandshakeResult {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for SSH handshake: %v", err)
	}

	result := make(chan sshHandshakeResult, 1)
	go func() {
		defer listener.Close()
		serverResult := make(chan error, 1)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				serverResult <- err
				return
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				serverResult <- err
				return
			}
			sshConn, _, _, err := ssh.NewServerConn(conn, config)
			if sshConn != nil {
				sshConn.Close()
			}
			serverResult <- err
		}()

		conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
		if err != nil {
			listener.Close()
			result <- sshHandshakeResult{clientErr: err, serverErr: <-serverResult}
			return
		}
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			conn.Close()
			result <- sshHandshakeResult{clientErr: err, serverErr: <-serverResult}
			return
		}
		clientConn, _, _, clientErr := ssh.NewClientConn(conn, listener.Addr().String(), &ssh.ClientConfig{
			User:            "test",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		})
		if clientConn != nil {
			clientConn.Close()
		}
		conn.Close()
		result <- sshHandshakeResult{clientErr: clientErr, serverErr: <-serverResult}
	}()
	return result
}

func waitSSHHandshake(t *testing.T, result <-chan sshHandshakeResult) sshHandshakeResult {
	t.Helper()
	select {
	case result := <-result:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SSH handshake")
		return sshHandshakeResult{}
	}
}

func requireSSHHandshakeSuccess(t *testing.T, result sshHandshakeResult) {
	t.Helper()
	if result.clientErr != nil || result.serverErr != nil {
		t.Fatalf("SSH handshake failed: client=%v server=%v", result.clientErr, result.serverErr)
	}
}
