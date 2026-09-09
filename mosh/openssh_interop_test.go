//go:build linux

package mosh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/client"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func startOpenSSH(t *testing.T) Config {
	t.Helper()
	daemon := "/usr/sbin/sshd"
	if _, err := os.Stat(daemon); err != nil {
		if os.Getenv("MOSH_TEST_REQUIRED") != "" {
			t.Fatal("OpenSSH server not installed")
		}
		t.Skip("OpenSSH server not installed")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	makeKey := func(name string) (string, ssh.PublicKey) {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		block, err := ssh.MarshalPrivateKey(private, "")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
		signer, _ := ssh.NewSignerFromKey(private)
		return path, signer.PublicKey()
	}
	host, hostPub := makeKey("host")
	identity, clientPub := makeKey("identity")
	authorized := filepath.Join(dir, "authorized_keys")
	os.WriteFile(authorized, ssh.MarshalAuthorizedKey(clientPub), 0600)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	known := filepath.Join(dir, "known_hosts")
	os.WriteFile(known, []byte(knownhosts.Line([]string{addr}, hostPub)+"\n"), 0600)
	config := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nStrictModes no\nUsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin yes\nAllowUsers %s\nLogLevel ERROR\n", port, host, filepath.Join(dir, "pid"), authorized, u.Username)
	if os.Geteuid() == 0 {
		config = strings.Replace(config, "UsePAM no", "UsePAM yes", 1)
	}
	path := filepath.Join(dir, "sshd_config")
	os.WriteFile(path, []byte(config), 0600)
	log, err := os.Create(filepath.Join(dir, "sshd.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(daemon, "-D", "-e", "-f", path)
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		log.Close()
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Logf("OpenSSH: %s", b)
		}
	})
	waitInterop(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
		return false
	}, func() string { b, _ := os.ReadFile(log.Name()); return "OpenSSH startup failed: " + string(b) })
	return Config{Config: client.Config{Destination: u.Username + "@" + addr, Identity: identity, KnownHosts: known}, MoshServer: referenceBinary(t, "mosh-server")}
}
