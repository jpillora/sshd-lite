//go:build !windows

package mosh

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

	"github.com/jpillora/sshd-lite/client"
	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func startServer(t *testing.T, enabled bool) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := sshd.Config{AuthType: "user:pass", KeySeed: "client-test", KeySeedEC: true, Shell: "/bin/sh", WorkDir: dir, LogQuiet: true, NoInheritEnv: true}
	if enabled {
		cfg.Attach = Attach
	}
	server, err := sshd.NewServer(cfg)
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
	return Config{Config: client.Config{Destination: "user@" + listener.Addr().String(), Password: "pass", KnownHosts: known}}, dir
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
	conn, err := c.connect(ctx)
	if err != nil {
		return 0, "", "", err
	}
	code, err := Run(ctx, conn, file, &stdout, ClientConfig{Server: c.MoshServer, Command: c.Command})
	return code, stdout.String(), stderr.String(), err
}

func TestMoshEndToEnd(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_SECRET", "must-not-inherit")
	c, dir := startServer(t, true)
	const workdirMarker = ".sshd-lite-mosh-workdir"
	if err := os.WriteFile(filepath.Join(dir, workdirMarker), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Shell transforms the marker, so an echoed command cannot pass the test.
			input := fmt.Sprintf("printf 'RESULT_%%s\\n' %d\ntest -f %s && printf 'WORKDIR_%%s\\n' OK\nprintf 'ENV_%%s_END\\n' \"$SSHD_LITE_TEST_SECRET\"\nexit 7\n", i, workdirMarker)
			code, out, stderr, err := runInput(t, c, input)
			if err != nil || code != 7 || !strings.Contains(out, fmt.Sprintf("RESULT_%d", i)) || !strings.Contains(out, "WORKDIR_OK") || strings.Contains(out, "must-not-inherit") || !strings.Contains(out, "ENV__END") {
				t.Errorf("code=%d out=%q stderr=%q err=%v", code, out, stderr, err)
			}
		}()
	}
	wg.Wait()
}

func TestMoshDisabledAndSSHAuthentication(t *testing.T) {
	c, _ := startServer(t, false)
	conn, err := c.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session, err := conn.NewSession()
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	output, err := session.CombinedOutput("echo SSH_ONLY")
	conn.Close()
	if err != nil || strings.TrimSpace(string(output)) != "SSH_ONLY" {
		t.Fatalf("SSH-only exec: %q %v", output, err)
	}
	c.Password = "wrong"
	if _, _, _, err := runInput(t, c, ""); err == nil {
		t.Fatal("wrong SSH password accepted")
	}
}

type Config struct {
	client.Config
	MoshServer string
}

func (c Config) connect(ctx context.Context) (*ssh.Client, error) {
	return client.Connect(ctx, c.Config)
}
func (c Config) target() (string, string, error) {
	user, addr, _ := strings.Cut(c.Destination, "@")
	return user, addr, nil
}
