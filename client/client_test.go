//go:build !windows

package client

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func startServer(t *testing.T, mosh bool) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	server, err := sshd.NewServer(sshd.Config{AuthType: "user:pass", KeySeed: "client-test", KeySeedEC: true, Shell: "/bin/sh", WorkDir: dir, Mosh: mosh, LogQuiet: true, NoInheritEnv: true})
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
			t.Error("shutdown hung")
		}
	})
	// Capture this test server's key and exercise normal known_hosts checking.
	known := filepath.Join(t.TempDir(), "known_hosts")
	connection, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "user", Auth: []ssh.AuthMethod{ssh.Password("pass")}, HostKeyCallback: func(host string, remote net.Addr, key ssh.PublicKey) error {
		return os.WriteFile(known, []byte(knownhosts.Line([]string{host}, key)+"\n"), 0600)
	}})
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	return Config{Destination: "user@" + listener.Addr().String(), Password: "pass", KnownHosts: known, Mosh: mosh}, dir
}

func runInput(t *testing.T, c Config, text string) (int, string, string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	file.WriteString(text)
	file.Seek(0, 0)
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Run(ctx, c, file, &stdout, &stderr)
	return code, stdout.String(), stderr.String(), err
}

func TestSSHExec(t *testing.T) {
	c, dir := startServer(t, false)
	c.Command = []string{"cat; pwd; printf error >&2; exit 7"}
	code, out, stderr, err := runInput(t, c, "SSH input\n")
	if err != nil || code != 7 || !strings.Contains(out, "SSH input\n") || !strings.Contains(out, dir) || stderr != "error" {
		t.Fatalf("code=%d out=%q stderr=%q err=%v", code, out, stderr, err)
	}
}

func TestMoshEndToEnd(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_SECRET", "must-not-inherit")
	c, dir := startServer(t, true)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Shell transforms the marker, so an echoed command cannot pass the test.
			input := fmt.Sprintf("printf 'RESULT_%%s\\n' %d\npwd\nprintf 'ENV_%%s_END\\n' \"$SSHD_LITE_TEST_SECRET\"\nexit 7\n", i)
			code, out, stderr, err := runInput(t, c, input)
			if err != nil || code != 7 || !strings.Contains(out, fmt.Sprintf("RESULT_%d", i)) || !strings.Contains(out, dir) || strings.Contains(out, "must-not-inherit") || !strings.Contains(out, "ENV__END") {
				t.Errorf("code=%d out=%q stderr=%q err=%v", code, out, stderr, err)
			}
		}()
	}
	wg.Wait()
}

func TestMoshDisabledAndSSHAuthentication(t *testing.T) {
	c, _ := startServer(t, false)
	c.Mosh = true
	if _, _, _, err := runInput(t, c, ""); err == nil || !strings.Contains(err.Error(), "rejected Mosh") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	c.Password = "wrong"
	if _, _, _, err := runInput(t, c, ""); err == nil {
		t.Fatal("wrong SSH password accepted")
	}
}

func TestHostKeyVerification(t *testing.T) {
	c, _ := startServer(t, false)
	c.KnownHosts = filepath.Join(t.TempDir(), "empty")
	os.WriteFile(c.KnownHosts, nil, 0600)
	c.Command = []string{"true"}
	if _, _, _, err := runInput(t, c, ""); err == nil {
		t.Fatal("unknown host key accepted")
	}
}
