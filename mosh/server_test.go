package mosh_test

import (
	"context"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/mosh"
	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
)

func TestMoshUDPBindFailureClosesSSHListener(t *testing.T) {
	server := newMoshTestServer(t)
	listener := listenMoshTest(t)
	address, err := net.ResolveUDPAddr("udp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenUDP("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	if err := server.StartWithContext(context.Background(), listener); err == nil {
		t.Fatal("UDP bind failure was ignored")
	}
	if c, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		c.Close()
		t.Fatal("SSH listener left open after UDP bind failure")
	}
}

func TestMoshBootstrapValidationAndShutdown(t *testing.T) {
	server := newMoshTestServer(t)
	listener := listenMoshTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.StartWithContext(ctx, listener) }()
	client := dialMoshTest(t, listener.Addr().String())
	defer client.Close()
	// The only supported Mosh bootstrap is the standard session exec command.
	ok, _, err := client.SendRequest("mosh@sshd-lite", true, []byte(`{"Cols":80,"Rows":24}`))
	if err != nil || ok {
		t.Fatalf("private JSON bootstrap still enabled: %v %v", ok, err)
	}
	run := func(command string) ([]byte, error) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestPty("xterm-256color", 24, 80, nil); err != nil {
			t.Fatal(err)
		}
		return session.CombinedOutput(command)
	}
	for _, command := range []string{"mosh-server new -p 0", "mosh-server new --invalid", "mosh-server new -l PATH=/tmp"} {
		output, err := run(command)
		if err == nil || strings.Contains(string(output), "MOSH CONNECT") {
			t.Fatalf("invalid bootstrap accepted: %q %q %v", command, output, err)
		}
	}
	output, err := run("echo SSH_FALLBACK")
	if err != nil || strings.TrimSpace(string(output)) != "SSH_FALLBACK" {
		t.Fatalf("ordinary exec did not fall through: %q %v", output, err)
	}
	output, err = run("mosh-server new -s -c 256 -l LANG=C.UTF-8")
	if err != nil {
		t.Fatalf("standard bootstrap: %q %v", output, err)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 4 || fields[0] != "MOSH" || fields[1] != "CONNECT" {
		t.Fatalf("bootstrap response: %q", output)
	}
	port, err := strconv.Atoi(fields[2])
	if err != nil || port != listener.Addr().(*net.TCPAddr).Port {
		t.Fatalf("wrong UDP port: %s %v", fields[2], err)
	}
	key, err := base64.RawStdEncoding.DecodeString(fields[3])
	if err != nil || len(key) != 16 {
		t.Fatalf("invalid session key encoding: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("shutdown did not release UDP: %v", err)
	}
	udp.Close()
}

func newMoshTestServer(t *testing.T) *sshd.Server {
	t.Helper()
	s, err := sshd.NewServer(sshd.Config{AuthType: "user:pass", KeySeed: "mosh-test", KeySeedEC: true, LogQuiet: true, Attach: mosh.Attach})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func listenMoshTest(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}
func dialMoshTest(t *testing.T, addr string) *ssh.Client {
	t.Helper()
	c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: "user", Auth: []ssh.AuthMethod{ssh.Password("pass")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
