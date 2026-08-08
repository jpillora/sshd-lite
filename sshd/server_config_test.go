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

func TestAuthTypeClassification(t *testing.T) {
	tests := []struct {
		name     string
		auth     string
		wantUser string
		wantPass string
		wantPair bool
	}{
		{name: "user and password", auth: "myuser:mypass", wantUser: "myuser", wantPass: "mypass", wantPair: true},
		{name: "password containing colons", auth: "myuser:a:b", wantUser: "myuser", wantPass: "a:b", wantPair: true},
		{name: "empty password", auth: "myuser:", wantUser: "myuser", wantPair: true},
		{name: "relative path", auth: "authorized_keys"},
		{name: "unix absolute path", auth: "/etc/ssh/authorized_keys"},
		// A drive-qualified path must never authorize a password: reading
		// "C:\keys" as user "C" would silently replace public key
		// authentication with a password of "\keys".
		{name: "windows backslash path", auth: `C:\ProgramData\ssh\authorized_keys`},
		{name: "windows forward slash path", auth: "C:/ProgramData/ssh/authorized_keys"},
		{name: "windows lowercase drive", auth: `d:\keys\authorized_keys`},
		// A bare drive letter is not a path, so the pair reading still wins.
		{name: "drive letter without separator", auth: "c:pass", wantUser: "c", wantPass: "pass", wantPair: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, pass, ok := parseUserPass(tt.auth)
			if ok != tt.wantPair {
				t.Fatalf("parseUserPass(%q) pair = %v, want %v", tt.auth, ok, tt.wantPair)
			}
			if !ok {
				return
			}
			if user != tt.wantUser || pass != tt.wantPass {
				t.Fatalf("parseUserPass(%q) = %q, %q, want %q, %q", tt.auth, user, pass, tt.wantUser, tt.wantPass)
			}
		})
	}
}

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

func TestFileAuthInitialLoadRejectsAuthorizedKeyOptions(t *testing.T) {
	signer, err := sshdkey.SignerFromSeed("file-auth-initial-options")
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}
	const sensitiveCommand = "INITIAL-SENSITIVE-FORCED-COMMAND-7216"
	path := filepath.Join(t.TempDir(), "authorized_keys")
	writeAuthFile(t, path, []byte(`command="`+sensitiveCommand+`" `+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))+"\n"))

	_, err = NewServer(fileAuthConfig(path, nil))
	if err == nil {
		t.Fatal("NewServer accepted an option-bearing authorized key")
	}
	if got := err.Error(); !strings.Contains(got, "initialize authorized keys") ||
		!strings.Contains(got, "line 1") ||
		!strings.Contains(got, "unsupported options") {
		t.Fatalf("NewServer error %q lacks clear authorized_keys option context", got)
	} else if strings.Contains(got, sensitiveCommand) {
		t.Fatalf("NewServer error exposed authorized_keys option data: %q", got)
	}
}

func TestPasswordAuthenticationLogsDoNotExposeCredentials(t *testing.T) {
	const (
		username           = "password-log-user"
		configuredPassword = "SERVER-PASSWORD-SENTINEL-6fe4d48c"
		attemptedPassword  = "ATTEMPTED-PASSWORD-SENTINEL-98d2a731"
	)

	tests := []struct {
		name         string
		password     string
		wantSuccess  bool
		wantLogEntry string
	}{
		{
			name:         "success",
			password:     configuredPassword,
			wantSuccess:  true,
			wantLogEntry: "User '" + username + "' authenticated with password",
		},
		{
			name:         "failure",
			password:     attemptedPassword,
			wantLogEntry: "Password authentication failed for user '" + username + "'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			server, err := NewServer(Config{
				AuthType:   username + ":" + configuredPassword,
				KeySeed:    "password-log-test",
				KeySeedEC:  true,
				LogVerbose: true,
				Logger:     logger,
			})
			if err != nil {
				t.Fatalf("NewServer failed: %v", err)
			}

			clientErr := runPasswordSSHHandshake(t, server, username, tt.password)
			if tt.wantSuccess && clientErr != nil {
				t.Fatalf("password authentication failed: %v", clientErr)
			}
			if !tt.wantSuccess && clientErr == nil {
				t.Fatal("password authentication unexpectedly succeeded")
			}

			output := logs.String()
			for _, secret := range []string{
				configuredPassword,
				attemptedPassword,
				username + ":" + configuredPassword,
				username + ":" + attemptedPassword,
			} {
				if strings.Contains(output, secret) {
					t.Fatalf("authentication logs exposed secret %q:\n%s", secret, output)
				}
			}
			if !strings.Contains(output, tt.wantLogEntry) {
				t.Fatalf("authentication logs lack useful username context %q:\n%s", tt.wantLogEntry, output)
			}
			if !tt.wantSuccess && !strings.Contains(output, "Failed to handshake") {
				t.Fatalf("failed handshake was not logged generically:\n%s", output)
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

func TestFileAuthOptionBearingReloadDeniesAndRecovers(t *testing.T) {
	signer, err := sshdkey.SignerFromSeed("file-auth-option-reload")
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}
	entry := ssh.MarshalAuthorizedKey(signer.PublicKey())
	path := filepath.Join(t.TempDir(), "authorized_keys")
	writeAuthFile(t, path, entry)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	server, err := NewServer(fileAuthConfig(path, logger))
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	requireSSHHandshakeSuccess(t, waitSSHHandshake(t, startSSHHandshake(t, server.sshConfig, signer)))

	const sensitiveCommand = "RELOAD-SENSITIVE-FORCED-COMMAND-1659"
	restricted := []byte(`command="` + sensitiveCommand + `",no-port-forwarding ` + strings.TrimSpace(string(entry)) + "\n")
	replaceAuthFile(t, path, restricted)
	requireFileAuth(t, server.sshConfig.PublicKeyCallback, signer.PublicKey(), false)
	_, err = server.sshConfig.VerifiedPublicKeyCallback(nil, signer.PublicKey(), nil, ssh.KeyAlgoED25519)
	if err == nil || err.Error() != "denied" {
		t.Fatalf("verified callback returned %v for option-bearing file, want denied", err)
	}

	denied := waitSSHHandshake(t, startSSHHandshake(t, server.sshConfig, signer))
	if denied.clientErr == nil {
		t.Fatal("SSH handshake authenticated a restricted key as unrestricted")
	}
	if got := denied.clientErr.Error(); strings.Contains(got, path) || strings.Contains(got, "authorized keys") || strings.Contains(got, sensitiveCommand) {
		t.Fatalf("client error exposed authorized_keys reload details: %v", denied.clientErr)
	}
	if got := logs.String(); !strings.Contains(got, "unsupported options") || strings.Contains(got, sensitiveCommand) {
		t.Fatalf("reload log lacks a safe cause or exposes option data: %s", got)
	}

	replaceAuthFile(t, path, entry)
	requireFileAuth(t, server.sshConfig.PublicKeyCallback, signer.PublicKey(), true)
	if _, err := server.sshConfig.VerifiedPublicKeyCallback(nil, signer.PublicKey(), nil, ssh.KeyAlgoED25519); err != nil {
		t.Fatalf("verified callback did not recover after clean replacement: %v", err)
	}
	requireSSHHandshakeSuccess(t, waitSSHHandshake(t, startSSHHandshake(t, server.sshConfig, signer)))
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
	const sensitiveCommand = "SIGNED-WINDOW-SENSITIVE-COMMAND-4382"
	replaceAuthFile(t, path, []byte(`command="`+sensitiveCommand+`" `+strings.TrimSpace(string(entry))+"\n"))
	close(resumeQuery)

	revoked := waitSSHHandshake(t, result)
	if revoked.clientErr == nil {
		t.Fatal("signed authentication succeeded with a key revoked after its query")
	}
	if strings.Contains(revoked.clientErr.Error(), path) || strings.Contains(revoked.clientErr.Error(), "authorized keys") || strings.Contains(revoked.clientErr.Error(), sensitiveCommand) {
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

func replaceAuthFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	temp, err := os.CreateTemp(filepath.Dir(path), ".authorized-keys-replacement-*")
	if err != nil {
		t.Fatalf("create authorized keys replacement: %v", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		t.Fatalf("chmod authorized keys replacement: %v", err)
	}
	if _, err := temp.Write(contents); err != nil {
		temp.Close()
		t.Fatalf("write authorized keys replacement: %v", err)
	}
	if err := temp.Close(); err != nil {
		t.Fatalf("close authorized keys replacement: %v", err)
	}
	if err := os.Rename(tempPath, path); err == nil {
		return
	}
	// Windows cannot replace an existing file with os.Rename. The callbacks
	// still fail closed during this short remove-and-rename fallback.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove old authorized keys file: %v", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		t.Fatalf("install authorized keys replacement: %v", err)
	}
}

func requireFileAuth(t *testing.T, callback func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error), publicKey ssh.PublicKey, wantAllowed bool) {
	t.Helper()
	if callback == nil {
		t.Fatal("server did not configure public key authentication")
	}
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

func runPasswordSSHHandshake(t *testing.T, server *Server, username, password string) error {
	t.Helper()
	serverConn, clientConn := tcpConnPair(t)
	if err := clientConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set password handshake deadline: %v", err)
	}

	serverDone := make(chan struct{})
	go func() {
		server.HandleConn(serverConn)
		close(serverDone)
	}()

	clientSSHConn, _, _, clientErr := ssh.NewClientConn(clientConn, "password-log-test", &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if clientSSHConn != nil {
		clientSSHConn.Close()
	}
	clientConn.Close()

	select {
	case <-serverDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for password SSH handshake")
	}
	return clientErr
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
