//go:build !windows

package mosh_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/mosh"
	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
)

type apiOutput struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *apiOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *apiOutput) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

// Only public packages participate: in-memory keys, a caller-owned listener,
// explicit SSH authentication/host verification, and ordinary Go I/O.
func apiServer(t *testing.T) (string, *ssh.ClientConfig, string) {
	t.Helper()
	makeKey := func() ([]byte, ssh.Signer) {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			t.Fatal(err)
		}
		block, err := ssh.MarshalPrivateKey(key, "")
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(block), signer
	}
	hostBytes, hostKey := makeKey()
	_, userKey := makeKey()
	dir := t.TempDir()
	server, err := sshd.NewServer(sshd.Config{Attach: mosh.Attach, KeyBytes: hostBytes, AuthKeys: []ssh.PublicKey{userKey.PublicKey()}, Shell: "/bin/sh", WorkDir: dir, NoInheritEnv: true, NoGlobalEnv: true, LogQuiet: true})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.StartWithContext(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("embedded server shutdown stalled")
		}
		udp, err := net.ListenPacket("udp", listener.Addr().String())
		if err != nil {
			t.Errorf("embedded server did not release UDP: %v", err)
		} else {
			udp.Close()
		}
	})
	return listener.Addr().String(), &ssh.ClientConfig{User: "api-user", Auth: []ssh.AuthMethod{ssh.PublicKeys(userKey)}, HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()), Timeout: time.Second}, dir
}
func awaitAPI(t *testing.T, check func() bool, describe func() string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal(describe())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func finishAPI(t *testing.T, s *mosh.Session) (int, error) {
	t.Helper()
	select {
	case <-s.Done():
		return s.Wait()
	case <-time.After(4 * time.Second):
		t.Fatal("programmatic session did not finish")
		return 0, nil
	}
}

func TestMoshPublicAPI(t *testing.T) {
	address, auth, dir := apiServer(t)
	for _, start := range []string{"dial", "existing-ssh"} {
		t.Run(start, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output := new(apiOutput)
			cfg := mosh.ClientConfig{Columns: 93, Rows: 31, Output: output, Prefix: 0x01020304}
			var session *mosh.Session
			var err error
			if start == "dial" {
				session, err = mosh.Dial(ctx, address, auth, cfg)
			} else {
				connection, dialErr := ssh.Dial("tcp", address, auth)
				if dialErr != nil {
					t.Fatal(dialErr)
				}
				session, err = mosh.Start(ctx, connection, cfg)
				if _, _, requestErr := connection.SendRequest("keepalive@test", true, nil); requestErr == nil {
					t.Fatal("bootstrap SSH connection still open")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			data := []byte("printf 'PUBLIC_%s\\n' API\n")
			if n, err := session.Write(data); err != nil || n != len(data) {
				t.Fatalf("write=%d %v", n, err)
			}
			clear(data) // The caller can immediately reuse its input buffer.
			awaitAPI(t, func() bool { return strings.Contains(output.text(), "PUBLIC_API") }, output.text)
			if err := session.Resize(91, 37); err != nil {
				t.Fatal(err)
			}
			sizeFile := filepath.Join(dir, "size-"+start)
			// A queued resize is asynchronous. Poll the actual remote PTY via Go input.
			awaitAPI(t, func() bool {
				_, err := fmt.Fprintf(session, "stty size > '%s'\n", sizeFile)
				if err != nil {
					return false
				}
				b, _ := os.ReadFile(sizeFile)
				return string(b) == "37 91\n"
			}, output.text)
			if _, err := io.WriteString(session, "exit 7\n"); err != nil {
				t.Fatal(err)
			}
			code, err := finishAPI(t, session)
			if code != 7 || err != nil {
				t.Fatalf("exit=%d err=%v output=%q", code, err, output.text())
			}
			if again, err := session.Wait(); again != 7 || err != nil {
				t.Fatal("Wait result was not stable")
			}
			if _, err := session.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("write after completion: %v", err)
			}
			if err := session.Resize(80, 24); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("resize after completion: %v", err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMoshPublicAPICancellation(t *testing.T) {
	address, auth, _ := apiServer(t)
	for _, action := range []string{"context", "close"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output := new(apiOutput)
			s, err := mosh.Dial(ctx, address, auth, mosh.ClientConfig{Command: []string{"/bin/sh", "-c", "printf STARTED; exec cat"}, Output: output})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			awaitAPI(t, func() bool { return strings.Contains(output.text(), "STARTED") }, output.text)
			if err := s.Resize(0, 24); err == nil {
				t.Fatal("invalid size accepted")
			}
			if action == "context" {
				cancel()
			} else {
				closed := make(chan struct{})
				go func() { s.Close(); close(closed) }()
				select {
				case <-closed:
				case <-time.After(4 * time.Second):
					t.Fatal("Close stalled")
				}
			}
			_, err = finishAPI(t, s)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation result: %v", err)
			}
		})
	}
}

type failedOutput struct{ err error }

func (w failedOutput) Write([]byte) (int, error) { return 0, w.err }
func TestMoshPublicAPIOutputError(t *testing.T) {
	address, auth, _ := apiServer(t)
	want := errors.New("application output stopped")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Keep the process alive after writing so PTY teardown cannot race output
	// delivery and turn this into an ordinary successful remote shutdown.
	command := []string{"/bin/sh", "-c", "printf OUTPUT; exec cat"}
	s, err := mosh.Dial(ctx, address, auth, mosh.ClientConfig{Command: command, Output: failedOutput{want}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = finishAPI(t, s)
	if !errors.Is(err, want) {
		t.Fatalf("lost output error: %v", err)
	}
}

func TestMoshPublicAPIValidationAndHostKey(t *testing.T) {
	address, auth, _ := apiServer(t)
	if _, err := mosh.Dial(context.Background(), address, nil, mosh.ClientConfig{}); err == nil {
		t.Fatal("missing SSH config accepted")
	}
	if _, err := mosh.Dial(context.Background(), address, auth, mosh.ClientConfig{Rows: -1}); err == nil {
		t.Fatal("invalid dimensions accepted")
	}
	if _, err := mosh.Start(context.Background(), nil, mosh.ClientConfig{}); err == nil {
		t.Fatal("nil SSH connection accepted")
	}
	denied := errors.New("untrusted host")
	bad := *auth
	bad.HostKeyCallback = func(string, net.Addr, ssh.PublicKey) error { return denied }
	if _, err := mosh.Dial(context.Background(), address, &bad, mosh.ClientConfig{}); err == nil || !strings.Contains(err.Error(), denied.Error()) {
		t.Fatalf("host-key verification not enforced: %v", err)
	}
	bad = *auth
	bad.Auth = nil
	if _, err := mosh.Dial(context.Background(), address, &bad, mosh.ClientConfig{}); err == nil {
		t.Fatal("missing authentication accepted")
	}
}

func TestMoshPublicAPIForwardsEscapeBytes(t *testing.T) {
	address, auth, dir := apiServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output := new(apiOutput)
	s, err := mosh.Dial(ctx, address, auth, mosh.ClientConfig{Command: []string{"/bin/sh", "-c", "stty raw -echo; printf RAW_READY; head -c 2 > received"}, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	awaitAPI(t, func() bool { return strings.Contains(output.text(), "RAW_READY") }, output.text)
	if _, err := s.Write([]byte{0x1e, '.'}); err != nil {
		t.Fatal(err)
	}
	code, err := finishAPI(t, s)
	if err != nil || code != 0 {
		t.Fatalf("escape bytes cancelled library session: %d %v", code, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "received"))
	if err != nil || !bytes.Equal(b, []byte{0x1e, '.'}) {
		t.Fatalf("escape bytes changed: %q %v", b, err)
	}
}

func TestMoshPublicAPICloseReleasesWriters(t *testing.T) {
	address, auth, _ := apiServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output := new(apiOutput)
	s, err := mosh.Dial(ctx, address, auth, mosh.ClientConfig{Command: []string{"/bin/sh", "-c", "printf BLOCKED_READY; sleep 30"}, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	awaitAPI(t, func() bool { return strings.Contains(output.text(), "BLOCKED_READY") }, output.text)
	writes := make(chan error, 2)
	for range 2 {
		go func() { _, err := s.Write(bytes.Repeat([]byte("x"), 8<<20)); writes <- err }()
	}
	sizes := make(chan struct{})
	go func() {
		defer close(sizes)
		for i := range 100 {
			if s.Resize(80+i%10, 24) != nil {
				return
			}
		}
	}()
	// With a blocked remote consumer, queued writes must not prevent cancellation.
	time.Sleep(50 * time.Millisecond)
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("Close blocked behind Write")
	}
	for range 2 {
		select {
		case err := <-writes:
			if err == nil {
				t.Fatal("oversized write unexpectedly completed without backpressure")
			}
		case <-time.After(time.Second):
			t.Fatal("queued writer did not stop")
		}
	}
	select {
	case <-sizes:
	case <-time.After(time.Second):
		t.Fatal("concurrent resize did not stop")
	}
}

// A peer can authenticate successfully and then never answer channel-open.
// Cancellation must cover that phase and release the transferred connection.
func TestMoshPublicAPICancelStalledBootstrap(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	opened := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			stopped <- err
			return
		}
		defer raw.Close()
		conn, channels, requests, err := ssh.NewServerConn(raw, config)
		if err != nil {
			stopped <- err
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		if _, ok := <-channels; !ok {
			stopped <- errors.New("no channel-open received")
			return
		}
		close(opened) // Deliberately neither accept nor reject this channel.
		_ = conn.Wait()
		stopped <- nil
	}()
	conn, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		s, err := mosh.Start(ctx, conn, mosh.ClientConfig{})
		if s != nil {
			s.Close()
		}
		result <- err
	}()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("bootstrap never opened SSH channel")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("bootstrap cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked on SSH channel-open")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("transferred SSH connection was not closed")
	}
}

type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestMoshPublicAPIShortOutput(t *testing.T) {
	address, auth, _ := apiServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Keep the process alive until the client observes the short write; otherwise
	// fast PTY teardown can discard its final output on some platforms.
	command := []string{"/bin/sh", "-c", "printf OUTPUT; exec cat"}
	s, err := mosh.Dial(ctx, address, auth, mosh.ClientConfig{Command: command, Output: shortOutput{}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = finishAPI(t, s)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short output silently dropped: %v", err)
	}
}
