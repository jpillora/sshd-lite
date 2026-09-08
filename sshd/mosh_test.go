package sshd

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh"
)

func TestMoshUDPBindFailureClosesSSHListener(t *testing.T) {
	server := newLifecycleTestServer(t, Config{Mosh: true})
	listener := listenLifecycleTest(t)
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
	server := newLifecycleTestServer(t, Config{Mosh: true})
	listener := listenLifecycleTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.StartWithContext(ctx, listener) }()
	client := dialLifecycleTest(t, listener.Addr().String())
	defer client.Close()
	for _, payload := range [][]byte{nil, []byte("invalid"), []byte(`{"Cols":65535,"Rows":65535}`), []byte(`{"Cols":80,"Rows":24,"Term":"bad\u0000term"}`)} {
		ok, _, err := client.SendRequest(mosh.RequestName, true, payload)
		if err != nil || ok {
			t.Fatalf("malformed bootstrap accepted: ok=%v err=%v", ok, err)
		}
	}
	request, _ := json.Marshal(mosh.Request{Cols: 80, Rows: 24})
	ok, payload, err := client.SendRequest(mosh.RequestName, true, request)
	if err != nil || !ok {
		t.Fatalf("bootstrap failed: ok=%v err=%v", ok, err)
	}
	var credentials mosh.Credentials
	if err := json.Unmarshal(payload, &credentials); err != nil {
		t.Fatal(err)
	}
	if credentials.Port != listener.Addr().(*net.TCPAddr).Port {
		t.Fatal("UDP and TCP ports differ")
	}
	if len(credentials.Key) != 24 {
		t.Fatal("wrong key length")
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
