package sshd_test

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"golang.org/x/crypto/ssh"
)

func TestUnknownAndMalformedRequestsReplyAndConnectionStaysUsable(t *testing.T) {
	server, err := sshtest.NewServer(
		sshtest.ServerWithNoAuth(),
		sshtest.ServerWithSFTP(true),
		sshtest.ServerWithTCPForwarding(true),
	)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	accepted, reply, err := client.SendRequest("unknown-global@test", true, []byte("ignored"))
	if err != nil {
		t.Fatalf("unknown global request: %v", err)
	}
	if accepted || len(reply) != 0 {
		t.Fatalf("unknown global reply = accepted %v, payload %x", accepted, reply)
	}

	if channel, _, err := client.OpenChannel("unknown-channel@test", nil); err == nil {
		_ = channel.Close()
		t.Fatal("unknown channel was accepted")
	} else {
		var openErr *ssh.OpenChannelError
		if !errors.As(err, &openErr) || openErr.Reason != ssh.UnknownChannelType {
			t.Fatalf("unknown channel error = %T %v", err, err)
		}
	}

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session for unknown request: %v", err)
	}
	accepted, err = session.SendRequest("unknown-session@test", true, []byte("ignored"))
	if err != nil {
		t.Fatalf("unknown session request: %v", err)
	}
	if accepted {
		t.Fatal("unknown session request was accepted")
	}
	out, err := session.CombinedOutput("echo same-session-usable")
	if err != nil {
		t.Fatalf("exec after unknown request on same session: %v", err)
	}
	if !strings.Contains(string(out), "same-session-usable") {
		t.Fatalf("exec output after unknown session request = %q", out)
	}

	subsystemPayloads := map[string][]byte{
		"missing length":    {0, 0, 0},
		"trailing bytes":    append(ssh.Marshal(struct{ Name string }{"sftp"}), 0),
		"unknown subsystem": ssh.Marshal(struct{ Name string }{"future-subsystem"}),
	}
	for name, payload := range subsystemPayloads {
		t.Run(name, func(t *testing.T) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("new session: %v", err)
			}
			defer session.Close()
			accepted, err := session.SendRequest("subsystem", true, payload)
			if err != nil {
				t.Fatalf("send subsystem request: %v", err)
			}
			if accepted {
				t.Fatal("malformed or unknown subsystem was accepted")
			}
			assertSSHExecUsable(t, client, "after-"+strings.ReplaceAll(name, " ", "-"))
		})
	}

	for _, requestType := range []string{"tcpip-forward", "cancel-tcpip-forward"} {
		accepted, _, err := client.SendRequest(requestType, true, []byte{0, 0, 0})
		if err != nil {
			t.Fatalf("malformed %s request: %v", requestType, err)
		}
		if accepted {
			t.Fatalf("malformed %s request was accepted", requestType)
		}
	}
	if channel, _, err := client.OpenChannel("direct-tcpip", []byte{0, 0, 0}); err == nil {
		_ = channel.Close()
		t.Fatal("malformed direct-tcpip channel was accepted")
	} else {
		var openErr *ssh.OpenChannelError
		if !errors.As(err, &openErr) || openErr.Reason == ssh.UnknownChannelType || openErr.Reason != ssh.ConnectionFailed {
			t.Fatalf("malformed direct-tcpip rejection = %T %v", err, err)
		}
	}
	assertSSHExecUsable(t, client, "after-malformed-forwarding")
	assertDirectTCPIPUsable(t, client)

	// A successful forward after the malformed traffic proves dispatcher state
	// remains usable; closing and rebinding proves its listener is cleaned up.
	forward, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("valid forward after malformed requests: %v", err)
	}
	addr := forward.Addr().String()
	if err := forward.Close(); err != nil {
		t.Fatalf("close valid forward: %v", err)
	}
	rebound := waitForTCPRebind(t, addr)
	_ = rebound.Close()
}

func assertDirectTCPIPUsable(t *testing.T, client *ssh.Client) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for direct-tcpip echo: %v", err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, err = conn.Write(buf)
		}
		serverDone <- err
	}()
	forwarded, err := client.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("valid direct-tcpip after malformed request: %v", err)
	}
	defer forwarded.Close()
	_ = forwarded.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := forwarded.Write([]byte("ping")); err != nil {
		t.Fatalf("write direct-tcpip payload: %v", err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(forwarded, got); err != nil {
		t.Fatalf("read direct-tcpip echo: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("direct-tcpip echo = %q", got)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("direct-tcpip echo server: %v", err)
	}
}

func assertSSHExecUsable(t *testing.T, client *ssh.Client, marker string) {
	t.Helper()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new follow-up session: %v", err)
	}
	defer session.Close()
	out, err := session.CombinedOutput("echo " + marker)
	if err != nil {
		t.Fatalf("follow-up exec: %v", err)
	}
	if !strings.Contains(string(out), marker) {
		t.Fatalf("follow-up exec output = %q, want %q", out, marker)
	}
}
