package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func trustKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, k, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	s, e := ssh.NewSignerFromKey(k)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func trustOptions(t *testing.T, path string) DialOptions {
	t.Helper()
	o, e := Resolve(context.Background(), DialOptions{Host: "example.test", User: "git", Sources: Sources{KnownHostsFiles: []string{path}}})
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func checkTrust(t *testing.T, o DialOptions, key ssh.PublicKey) error {
	t.Helper()
	cb, e := prepareTrust(context.Background(), o)
	if e != nil {
		return e
	}
	return cb("ignored", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key)
}
func TestTrustConfirmPersistAndRefuseChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "known")
	o := trustOptions(t, p)
	key := trustKey(t)
	calls := 0
	o.Interaction.ConfirmHost = func(ctx context.Context, k HostKey) (bool, error) {
		calls++
		if k.Fingerprint != ssh.FingerprintSHA256(key.PublicKey()) {
			t.Fatal("wrong fingerprint")
		}
		return true, nil
	}
	o.resolved.hash = true
	if e := checkTrust(t, o, key.PublicKey()); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(p)
	if e != nil || !bytes.HasPrefix(raw, []byte("|1|")) {
		t.Fatalf("saved %q %v", raw, e)
	}
	if e := checkTrust(t, o, key.PublicKey()); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("trusted key prompted again")
	}
	other := trustKey(t)
	if e := checkTrust(t, o, other.PublicKey()); !errors.Is(e, ErrHostKeyRejected) {
		t.Fatalf("changed key=%v", e)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(raw, after) || calls != 1 {
		t.Fatal("changed key was prompted or replaced")
	}
	native, e := knownhosts.New(p)
	if e != nil {
		t.Fatal(e)
	}
	if e := native("example.test:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, key.PublicKey()); e != nil {
		t.Fatal(e)
	}
}
func TestTrustRefusalAndConcurrentEntries(t *testing.T) {
	key := trustKey(t)
	for _, kind := range []string{"no", "batch", "nil", "foreign-lock", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "known")
			o := trustOptions(t, p)
			o.Interaction.ConfirmHost = func(context.Context, HostKey) (bool, error) { return kind != "no", nil }
			switch kind {
			case "batch":
				o.resolved.batch = true
			case "nil":
				o.Interaction.ConfirmHost = nil
			case "foreign-lock":
				os.WriteFile(p+".lock", []byte("foreign"), 0600)
			case "revoked":
				os.WriteFile(p, []byte("@revoked "+knownhosts.Line([]string{"example.test"}, key.PublicKey())+"\n"), 0600)
			}
			if e := checkTrust(t, o, key.PublicKey()); e == nil {
				t.Fatal("refusal succeeded")
			}
			if kind != "revoked" {
				if _, e := os.Stat(p); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("refusal wrote trust")
				}
			}
			if kind == "foreign-lock" {
				b, _ := os.ReadFile(p + ".lock")
				if string(b) != "foreign" {
					t.Fatal("foreign lock changed")
				}
			}
		})
	}
	p := filepath.Join(t.TempDir(), "known")
	o := trustOptions(t, p)
	other := trustKey(t)
	first := knownhosts.Line([]string{"other.test"}, other.PublicKey()) + "\n"
	o.Interaction.ConfirmHost = func(context.Context, HostKey) (bool, error) { return true, os.WriteFile(p, []byte(first), 0640) }
	if e := checkTrust(t, o, key.PublicKey()); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), first) {
		t.Fatal("lost concurrent other-host entry")
	}
	info, _ := os.Stat(p)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
}
func TestTrustRechecksRevocationAfterConfirmation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "known")
	o := trustOptions(t, p)
	key := trustKey(t)
	o.Interaction.ConfirmHost = func(context.Context, HostKey) (bool, error) {
		return true, os.WriteFile(p, []byte("@revoked "+knownhosts.Line([]string{"example.test"}, key.PublicKey())+"\n"), 0600)
	}
	if e := checkTrust(t, o, key.PublicKey()); !errors.Is(e, ErrHostKeyRejected) {
		t.Fatalf("revocation error=%v", e)
	}
}

func TestTrustCertificateAuthorityIsNotAnOrdinaryHostKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "known")
	ca, host := trustKey(t), trustKey(t)
	line := "@cert-authority " + knownhosts.Line([]string{"example.test"}, ca.PublicKey()) + "\n"
	if err := os.WriteFile(p, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	o := trustOptions(t, p)
	o.Interaction.ConfirmHost = func(context.Context, HostKey) (bool, error) {
		t.Fatal("CA-only trust must not prompt for an ordinary key")
		return true, nil
	}
	if err := checkTrust(t, o, ca.PublicKey()); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("ordinary CA key: %v", err)
	}
	cert := &ssh.Certificate{Key: host.PublicKey(), CertType: ssh.HostCert,
		ValidPrincipals: []string{"example.test"}, ValidBefore: ssh.CertTimeInfinity}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	if err := checkTrust(t, o, cert); err != nil {
		t.Fatalf("signed host certificate: %v", err)
	}
	line += "@revoked " + knownhosts.Line([]string{"*"}, ca.PublicKey()) + "\n"
	if err := os.WriteFile(p, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkTrust(t, o, cert); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("revoked CA: %v", err)
	}
}
