//go:build !windows

package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func keyForTest(t *testing.T) (ed25519.PrivateKey, ssh.Signer) {
	t.Helper()
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	s, e := ssh.NewSignerFromKey(key)
	if e != nil {
		t.Fatal(e)
	}
	return key, s
}
func TestIdentityAgentSelectionAndEncryptedKey(t *testing.T) {
	d := t.TempDir()
	key, signer := keyForTest(t)
	_, other := keyForTest(t)
	path := filepath.Join(d, "identity")
	block, err := ssh.MarshalPrivateKeyWithPassphrase(key, "test", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, pem.EncodeToMemory(block), 0600)
	socket := filepath.Join(d, "agent.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ring := agent.NewKeyring()
	ring.Add(agent.AddedKey{PrivateKey: key})
	done := make(chan error, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		done <- agent.ServeAgent(ring, c)
	}()
	o, err := Resolve(context.Background(), DialOptions{Host: "example", User: "git", IdentityFiles: []string{path}, Sources: Sources{AgentSocket: socket}})
	if err != nil {
		t.Fatal(err)
	}
	owner := &authOwner{}
	agents, err := owner.agent(context.Background(), socket)
	if err != nil {
		t.Fatal(err)
	}
	locked := &fileSigner{public: signer.PublicKey(), data: pem.EncodeToMemory(block), ctx: context.Background(), options: o}
	selected := orderedSigners(append([]ssh.Signer{other}, agents...), []ssh.Signer{locked}, true)
	if len(selected) != 1 {
		t.Fatalf("selected %d", len(selected))
	}
	sig, err := selected[0].Sign(rand.Reader, []byte("proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.PublicKey().Verify([]byte("proof"), sig); err != nil {
		t.Fatal(err)
	}
	owner.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent did not close")
	}
	locked.options.Passphrase = []byte("secret")
	if _, err := locked.Sign(rand.Reader, []byte("proof")); err != nil {
		t.Fatal(err)
	}
}
func TestAgentCancellation(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent")
	l, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &authOwner{}
	done := make(chan error, 1)
	go func() { _, e := owner.agent(ctx, socket); owner.close(); done <- e }()
	c := <-accepted
	defer c.Close()
	cancel()
	select {
	case e := <-done:
		if e != context.Canceled {
			t.Fatalf("error=%v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled agent ignored cancellation")
	}
}

func TestDialConfiguredAliasAndKey(t *testing.T) {
	d := t.TempDir()
	key, identity := keyForTest(t)
	_, hostKey := keyForTest(t)
	block, e := ssh.MarshalPrivateKey(key, "test")
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "key")
	os.WriteFile(p, pem.EncodeToMemory(block), 0600)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	conf := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() != "alias-user" || !bytes.Equal(k.Marshal(), identity.PublicKey().Marshal()) {
			return nil, fmt.Errorf("wrong identity")
		}
		return nil, nil
	}}
	conf.AddHostKey(hostKey)
	done := make(chan error, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		conn, ch, rq, e := ssh.NewServerConn(c, conf)
		if e != nil {
			done <- e
			return
		}
		go ssh.DiscardRequests(rq)
		for c := range ch {
			c.Reject(ssh.UnknownChannelType, "none")
		}
		done <- conn.Close()
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	known := filepath.Join(d, "known")
	os.WriteFile(known, []byte(knownhosts.Line([]string{l.Addr().String()}, hostKey.PublicKey())+"\n"), 0600)
	config := filepath.Join(d, "config")
	os.WriteFile(config, []byte("Host alias\nHostName "+host+"\nPort "+port+"\nUser alias-user\nIdentityFile "+p+"\nIdentitiesOnly yes\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := Dial(ctx, DialOptions{Host: "alias", Sources: Sources{Directory: d, ConfigFiles: []string{config}, KnownHostsFiles: []string{known}}})
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server not closed")
	}
}
