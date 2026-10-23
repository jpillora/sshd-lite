package client

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestResolvedConfigurationRejectsEditsBeforeDial(t *testing.T) {
	d := t.TempDir()
	input := DialOptions{Host: "old.example", User: "test", IdentityFiles: []string{filepath.Join(d, "key")}, Sources: Sources{
		Home: d, Directory: d, LocalUser: "local", AgentSocket: filepath.Join(d, "agent"),
		ConfigFiles: []string{filepath.Join(d, "config")}, KnownHostsFiles: []string{filepath.Join(d, "known")},
		Environment: map[string]string{"LANG": "C"},
	}}
	if err := os.WriteFile(input.Sources.ConfigFiles[0], []byte("SendEnv LANG\nIdentitiesOnly yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	edits := map[string]func(*DialOptions){
		"host":                 func(o *DialOptions) { o.Host = "127.0.0.1" },
		"user":                 func(o *DialOptions) { o.User = "another" },
		"port":                 func(o *DialOptions) { o.Port = "2222" },
		"timeout":              func(o *DialOptions) { o.Timeout = time.Second },
		"identity":             func(o *DialOptions) { o.IdentityFiles[0] += ".other" },
		"home":                 func(o *DialOptions) { o.Sources.Home += "-other" },
		"directory":            func(o *DialOptions) { o.Sources.Directory += "-other" },
		"local-user":           func(o *DialOptions) { o.Sources.LocalUser = "another" },
		"agent":                func(o *DialOptions) { o.Sources.AgentSocket += "-other" },
		"config":               func(o *DialOptions) { o.Sources.ConfigFiles[0] += "-other" },
		"known-hosts":          func(o *DialOptions) { o.Sources.KnownHostsFiles[0] += "-other" },
		"source-environment":   func(o *DialOptions) { o.Sources.Environment["LANG"] = "other" },
		"selected-environment": func(o *DialOptions) { o.SendEnvironment[0].Value = "other" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			o, err := Resolve(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			edit(&o)
			o.HostKeyCallback = func(string, net.Addr, ssh.PublicKey) error {
				t.Error("edited configuration reached handshake")
				return nil
			}
			if _, err := Resolve(context.Background(), o); !errors.Is(err, ErrResolvedOptionsChanged) {
				t.Fatalf("Resolve error=%v", err)
			}
			if c, err := Dial(context.Background(), o); !errors.Is(err, ErrResolvedOptionsChanged) {
				if c != nil {
					c.Close()
				}
				t.Fatalf("Dial error=%v", err)
			}
		})
	}
}

func TestResolvedSourcesAreDetachedAndCredentialsRemainPerAttempt(t *testing.T) {
	d := t.TempDir()
	input := DialOptions{Host: "example", User: "test", Sources: Sources{
		Directory: d, ConfigFiles: []string{filepath.Join(d, "optional")},
		KnownHostsFiles: []string{filepath.Join(d, "known")}, Environment: map[string]string{"LANG": "C"},
	}}
	o, err := Resolve(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Sources.ConfigFiles[0] += "-changed"
	input.Sources.KnownHostsFiles[0] += "-changed"
	input.Sources.Environment["LANG"] = "changed"
	o.Password = "per-attempt"
	o.Passphrase = []byte("per-attempt")
	o.Auth = []ssh.AuthMethod{ssh.Password("per-attempt")}
	o.Interaction = TerminalInteraction()
	o.HostKeyCallback = ssh.FixedHostKey(trustKey(t).PublicKey())
	if _, err := Resolve(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if o.Sources.Environment["LANG"] != "C" {
		t.Fatal("source environment aliases original input")
	}
	// Nil and empty explicit trust lists have different authority semantics.
	fresh, err := Resolve(context.Background(), DialOptions{Host: "example", User: "test"})
	if err != nil {
		t.Fatal(err)
	}
	fresh.Sources.KnownHostsFiles = []string{}
	if _, err := Resolve(context.Background(), fresh); !errors.Is(err, ErrResolvedOptionsChanged) {
		t.Fatalf("trust list edit: %v", err)
	}
}

func TestResolvedConfigurationConcurrentReuseWithHostKeyAlias(t *testing.T) {
	d := t.TempDir()
	key := trustKey(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	configFile, knownFile := filepath.Join(d, "config"), filepath.Join(d, "known")
	config := "Host work\n HostName " + host + "\n Port " + port + "\n User test\n HostKeyAlias trusted.example\n"
	if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{net.JoinHostPort("trusted.example", port)}, key.PublicKey()) + "\n"
	if err := os.WriteFile(knownFile, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := Resolve(context.Background(), DialOptions{Host: "work", Sources: Sources{Directory: d, ConfigFiles: []string{configFile}, KnownHostsFiles: []string{knownFile}}})
	if err != nil {
		t.Fatal(err)
	}
	server := &ssh.ServerConfig{NoClientAuth: true}
	server.AddHostKey(key)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	serverDone := make(chan error, 1)
	go func() {
		for range 2 {
			c, err := listener.Accept()
			if err != nil {
				serverDone <- err
				return
			}
			// Both clients finish immediately after authentication.
			sc, _, _, err := ssh.NewServerConn(c, server)
			if err != nil {
				c.Close()
				serverDone <- err
				return
			}
			sc.Close()
		}
		serverDone <- nil
	}()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt := o
			attempt.Password = "per-attempt"
			c, err := Dial(ctx, attempt)
			if err != nil {
				t.Error(err)
				return
			}
			c.Close()
		}()
	}
	wg.Wait()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(ctx, o); err != nil {
		t.Fatalf("reuse mutated configuration: %v", err)
	}
}
