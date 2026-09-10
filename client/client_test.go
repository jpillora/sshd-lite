//go:build !windows

package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
	sshdkey "github.com/jpillora/sshd-lite/sshd/key"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func startServer(t *testing.T) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	server, err := sshd.NewServer(sshd.Config{AuthType: "user:pass", KeySeed: "client-test", KeySeedEC: true, Shell: "/bin/sh", WorkDir: dir, LogQuiet: true, NoInheritEnv: true})
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
	return Config{Destination: "user@" + listener.Addr().String(), Password: "pass", KnownHosts: known}, dir
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
	c, dir := startServer(t)
	c.Command = []string{"cat; pwd; printf error >&2; exit 7"}
	code, out, stderr, err := runInput(t, c, "SSH input\n")
	if err != nil || code != 7 || !strings.Contains(out, "SSH input\n") || !strings.Contains(out, dir) || stderr != "error" {
		t.Fatalf("code=%d out=%q stderr=%q err=%v", code, out, stderr, err)
	}
}

func TestStrictHostKeyVerification(t *testing.T) {
	c, _ := startServer(t)
	c.KnownHosts = filepath.Join(t.TempDir(), "empty")
	os.WriteFile(c.KnownHosts, nil, 0600)
	c.StrictHosts = true
	c.Command = []string{"true"}
	if _, _, _, err := runInput(t, c, ""); err == nil || !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("non-interactive unknown host error = %v", err)
	}
}

func TestDefaultAddsUnknownHostKey(t *testing.T) {
	c, _ := startServer(t)
	c.KnownHosts = filepath.Join(t.TempDir(), "known_hosts")
	c.Command = []string{"true"}
	if code, _, _, err := runInput(t, c, ""); err != nil || code != 0 {
		t.Fatalf("accepted connection: code=%d err=%v", code, err)
	}
	callback, err := knownhosts.New(c.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(c.KnownHosts); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts mode: info=%v err=%v", info, err)
	}
	_ = callback
}

func TestDefaultKnownHostsUsesXDGState(t *testing.T) {
	c, _ := startServer(t)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	c.KnownHosts = ""
	c.Command = []string{"true"}
	if code, _, _, err := runInput(t, c, ""); err != nil || code != 0 {
		t.Fatalf("first connection: code=%d err=%v", code, err)
	}
	filename := filepath.Join(state, "sshd-lite", "known_hosts")
	if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private known_hosts mode: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Dir(filename)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private state directory mode: info=%v err=%v", info, err)
	}
}

func TestKnownHostsPathSelection(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", state)

	path, err := (Config{}).knownHostsPath()
	if err != nil || path != filepath.Join(state, "sshd-lite", "known_hosts") {
		t.Fatalf("XDG known_hosts path = %q, %v", path, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	path, err = (Config{}).knownHostsPath()
	if err != nil || path != filepath.Join(home, ".local", "state", "sshd-lite", "known_hosts") {
		t.Fatalf("fallback known_hosts path = %q, %v", path, err)
	}
	path, err = (Config{ShareKnownHosts: true}).knownHostsPath()
	if err != nil || path != filepath.Join(home, ".ssh", "known_hosts") {
		t.Fatalf("shared known_hosts path = %q, %v", path, err)
	}
	if _, err := (Config{KnownHosts: "custom", ShareKnownHosts: true}).knownHostsPath(); err == nil {
		t.Fatal("--known-hosts and --share-known-hosts were accepted together")
	}
}

func TestMismatchedHostKeyStillRequiresConfirmation(t *testing.T) {
	c, _ := startServer(t)
	wrong := testPublicKey(t, "unaccepted-wrong-host")
	c.KnownHosts = filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(c.KnownHosts, []byte(knownhosts.Line([]string{strings.TrimPrefix(c.Destination, "user@")}, wrong)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Command = []string{"true"}
	if _, _, _, err := runInput(t, c, ""); err == nil || !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("non-interactive changed host error = %v", err)
	}
}

func TestDefaultAddsUnknownHostCertificateAuthority(t *testing.T) {
	ca := testSigner(t, "certificate-authority")
	host := testSigner(t, "certified-host")
	cert := &ssh.Certificate{
		Key:             host.PublicKey(),
		CertType:        ssh.HostCert,
		KeyId:           "test host",
		ValidPrincipals: []string{"example.test"},
		ValidAfter:      0,
		ValidBefore:     ssh.CertTimeInfinity,
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(filename, []byte("# retained\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	callback, err := knownHostsCallback(filename, false, false)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := callback("example.test:22", remote, cert); err != nil {
		t.Fatalf("trust unknown host certificate: %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || !strings.Contains(string(data), "# retained\n@cert-authority example.test ") {
		t.Fatalf("saved certificate authority = %q, %v", data, err)
	}
	check, err := knownhosts.New(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := check("example.test:22", remote, cert); err != nil {
		t.Fatalf("saved certificate authority rejected certificate: %v", err)
	}

	revokedFile := filepath.Join(t.TempDir(), "known_hosts")
	revokedLine := "@revoked example.test " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert))) + "\n"
	if err := os.WriteFile(revokedFile, []byte(revokedLine), 0o600); err != nil {
		t.Fatal(err)
	}
	revokedCheck, err := knownHostsCallback(revokedFile, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := revokedCheck("example.test:22", remote, cert); err == nil || !strings.Contains(strings.ToLower(err.Error()), "revoked") {
		t.Fatalf("revoked host certificate error = %v", err)
	}
}

func TestAcceptReplacesMismatchedHostKey(t *testing.T) {
	c, _ := startServer(t)
	wrong := testPublicKey(t, "wrong-host")
	c.KnownHosts = filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(c.KnownHosts, []byte("# keep this comment\n"+knownhosts.Line([]string{strings.TrimPrefix(c.Destination, "user@")}, wrong)+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	c.Accept = true
	c.Command = []string{"true"}
	if code, _, _, err := runInput(t, c, ""); err != nil || code != 0 {
		t.Fatalf("accepted replacement: code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(c.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# keep this comment") || strings.Contains(string(data), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(wrong)))) {
		t.Fatalf("known_hosts replacement = %q", data)
	}
	if info, err := os.Stat(c.KnownHosts); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("known_hosts mode: info=%v err=%v", info, err)
	}
}

func TestAcceptNeverOverridesRevokedHostKey(t *testing.T) {
	c, _ := startServer(t)
	data, err := os.ReadFile(c.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.KnownHosts, append([]byte("@revoked "), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Accept = true
	c.Command = []string{"true"}
	if _, _, _, err := runInput(t, c, ""); err == nil || !strings.Contains(strings.ToLower(err.Error()), "revoked") {
		t.Fatalf("revoked key error = %v", err)
	}
}

func TestHostKeyPromptAndAnswers(t *testing.T) {
	key := testPublicKey(t, "prompt")
	for _, mismatch := range []bool{false, true} {
		var out bytes.Buffer
		writeHostKeyPrompt(&out, "example:22", key, mismatch)
		if mismatch {
			if !strings.Contains(out.String(), "IDENTIFICATION HAS CHANGED") || !strings.Contains(out.String(), "intercepting") {
				t.Fatalf("mismatch warning is not prominent: %q", out.String())
			}
		} else if !strings.Contains(out.String(), "authenticity") || strings.Contains(out.String(), "HAS CHANGED") {
			t.Fatalf("unknown-host warning = %q", out.String())
		}
		if !strings.Contains(out.String(), ssh.FingerprintSHA256(key)) {
			t.Fatalf("fingerprint missing: %q", out.String())
		}
	}
	for _, tt := range []struct {
		input string
		want  bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"anything\n", false}} {
		got, err := readConfirmation(strings.NewReader(tt.input))
		if err != nil || got != tt.want {
			t.Fatalf("answer %q = %v, %v", tt.input, got, err)
		}
	}
}

func TestKnownHostsReplacementPreservesOtherHostsOnLine(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "known_hosts")
	oldKey := testPublicKey(t, "shared-old")
	newKey := testPublicKey(t, "shared-new")
	line := knownhosts.Line([]string{"changed.example:22", "other.example:22"}, oldKey)
	if err := os.WriteFile(filename, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := []knownhosts.KnownKey{{Key: oldKey, Filename: filename, Line: 1}}
	if err := updateKnownHosts(filename, "changed.example:22", newKey, stale); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "other.example") || !strings.Contains(text, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(oldKey)))) {
		t.Fatalf("other host binding was lost: %q", text)
	}
	if !strings.Contains(text, "changed.example") || !strings.Contains(text, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newKey)))) {
		t.Fatalf("replacement binding missing: %q", text)
	}
}

func TestKnownHostsReplacementPreservesSymlinkAndWildcard(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "known_hosts.real")
	linkname := filepath.Join(dir, "known_hosts")
	oldKey := testPublicKey(t, "wildcard-old")
	newKey := testPublicKey(t, "wildcard-new")
	line := "@cert-authority *.example " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(oldKey)))
	if err := os.WriteFile(filename, []byte("# retained\n"+line+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(filename), linkname); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stale := []knownhosts.KnownKey{{Key: oldKey, Filename: linkname, Line: 2}}
	if err := updateKnownHosts(linkname, "changed.example", newKey, stale); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(linkname); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("known_hosts symlink replaced: info=%v err=%v", info, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"# retained", "@cert-authority !changed.example,*.example", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(oldKey))), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newKey)))} {
		if !strings.Contains(text, want) {
			t.Fatalf("known_hosts update omitted %q: %q", want, text)
		}
	}
	if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("known_hosts target mode: info=%v err=%v", info, err)
	}
}

func TestKnownHostsReplacementExcludesChangedHostFromRetainedWildcard(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "known_hosts")
	oldKey := testPublicKey(t, "overlapping-wildcard-old")
	newKey := testPublicKey(t, "overlapping-wildcard-new")
	line := knownhosts.Line([]string{"host.example", "*.example"}, oldKey)
	if err := os.WriteFile(filename, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := []knownhosts.KnownKey{{Key: oldKey, Filename: filename, Line: 1}}
	if err := updateKnownHosts(filename, "host.example", newKey, stale); err != nil {
		t.Fatal(err)
	}
	callback, err := knownhosts.New(filename)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := callback("host.example:22", remote, oldKey); err == nil {
		t.Fatal("changed host still accepts the old wildcard key")
	}
	if err := callback("host.example:22", remote, newKey); err != nil {
		t.Fatalf("changed host rejected replacement key: %v", err)
	}
	if err := callback("other.example:22", remote, oldKey); err != nil {
		t.Fatalf("other wildcard host rejected retained key: %v", err)
	}
}

func testPublicKey(t *testing.T, seed string) ssh.PublicKey {
	return testSigner(t, seed).PublicKey()
}

func testSigner(t *testing.T, seed string) ssh.Signer {
	t.Helper()
	b, err := sshdkey.GenerateKey(seed, true)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
