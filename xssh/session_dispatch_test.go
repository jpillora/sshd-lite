package xssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jpillora/sftp"
	"golang.org/x/crypto/ssh"
)

func sessionProtocolClient(t *testing.T, cfg *Config) *ssh.Client {
	t.Helper()
	cfg.ChannelHandlers = map[string]ChannelHandler{
		SessionChannelType: func(c Conn, ch ssh.NewChannel) error { return c.HandleSessionChannel(ch) },
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	server := &ssh.ServerConfig{NoClientAuth: true}
	server.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		conn, channels, requests, err := ssh.NewServerConn(c, server)
		if err != nil {
			c.Close()
			done <- err
			return
		}
		NewConn(conn, channels, requests, cfg).Serve()
		conn.Close()
		done <- nil
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()), Timeout: time.Second})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("session work did not stop")
		}
	})
	return client
}

func rawSession(t *testing.T, client *ssh.Client) ssh.Channel {
	t.Helper()
	channel, requests, err := client.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(requests)
	t.Cleanup(func() { channel.Close() })
	return channel
}

func requireSessionReply(t *testing.T, ch ssh.Channel, name string, payload []byte, want bool) {
	t.Helper()
	ok, err := ch.SendRequest(name, true, payload)
	if err != nil || ok != want {
		t.Fatalf("%s reply=%v err=%v want=%v", name, ok, err, want)
	}
}

func programRequest(name string) (string, []byte) {
	switch name {
	case "shell":
		return name, nil
	case "exec":
		return name, ssh.Marshal(struct{ Command string }{"wait"})
	default:
		return "subsystem", ssh.Marshal(struct{ Name string }{name})
	}
}

func TestSessionRejectsDuplicateAndMixedPrograms(t *testing.T) {
	for _, first := range []string{"shell", "exec", "custom", "sftp"} {
		t.Run(first, func(t *testing.T) {
			var starts atomic.Int32
			handler := func(*Session, *Request) error { starts.Add(1); return nil }
			cfg := &Config{NoGlobalEnv: true, SFTP: true, WorkingDirectory: t.TempDir(),
				SessionRequestHandlers: map[string]SessionRequestHandler{
					ShellRequestType: handler, ExecRequestType: handler, EnvRequestType: handleEnv,
					PTYRequestType: handlePtyReq, WindowChangeRequestType: handleWindowChange,
				}, SubsystemHandlers: map[string]SubsystemHandler{"custom": handler},
			}
			ch := rawSession(t, sessionProtocolClient(t, cfg))
			requireSessionReply(t, ch, "env", ssh.Marshal(struct{ Name, Value string }{"BEFORE", "yes"}), true)
			name, payload := programRequest(first)
			requireSessionReply(t, ch, name, payload, true)
			for _, second := range []string{"shell", "exec", "custom", "sftp"} {
				name, payload := programRequest(second)
				requireSessionReply(t, ch, name, payload, false)
			}
			// No-reply requests must obey the same admission rules.
			name, payload = programRequest("exec")
			if _, err := ch.SendRequest(name, false, payload); err != nil {
				t.Fatal(err)
			}
			requireSessionReply(t, ch, "env", ssh.Marshal(struct{ Name, Value string }{"LATE", "no"}), false)
			requireSessionReply(t, ch, "pty-req", ssh.Marshal(ptyRequestPayload{Term: "xterm", Columns: 80, Rows: 24}), false)
			requireSessionReply(t, ch, "window-change", ssh.Marshal(windowChangePayload{Columns: 100, Rows: 30}), true)
			wantStarts := int32(1)
			if first == "sftp" {
				wantStarts = 0
			}
			if starts.Load() != wantStarts {
				t.Fatalf("launched %d custom programs, want %d", starts.Load(), wantStarts)
			}
			if first == "sftp" {
				client, err := sftp.NewClientPipe(ch, ch)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				if _, err := client.Stat("."); err != nil {
					t.Fatalf("rejected duplicate damaged running SFTP: %v", err)
				}
			}
		})
	}
}

func TestSessionFailedRequestsLeaveProgramAdmissionOpen(t *testing.T) {
	var starts atomic.Int32
	cfg := &Config{Session: true, Shell: filepath.Join(t.TempDir(), "missing-shell"), NoGlobalEnv: true,
		SubsystemHandlers: map[string]SubsystemHandler{"valid": func(*Session, *Request) error { starts.Add(1); return nil }},
	}
	ch := rawSession(t, sessionProtocolClient(t, cfg))
	requireSessionReply(t, ch, "exec", []byte{0, 0, 0, 9}, false)
	requireSessionReply(t, ch, "subsystem", []byte{0, 0, 0, 9}, false)
	requireSessionReply(t, ch, "subsystem", marshalSubsystemName("unknown"), false)
	requireSessionReply(t, ch, "shell", nil, false)
	requireSessionReply(t, ch, "subsystem", marshalSubsystemName("valid"), true)
	requireSessionReply(t, ch, "shell", nil, false)
	if starts.Load() != 1 {
		t.Fatalf("valid subsystem started %d times", starts.Load())
	}
}

func TestSessionCustomReplyControlsAdmission(t *testing.T) {
	var calls atomic.Int32
	cfg := &Config{NoGlobalEnv: true, SessionRequestHandlers: map[string]SessionRequestHandler{
		ExecRequestType: func(_ *Session, req *Request) error {
			if calls.Add(1) == 1 {
				return req.Reply(false, nil)
			}
			if err := req.Reply(true, nil); err != nil {
				return err
			}
			return fmt.Errorf("failure after acceptance")
		},
	}}
	ch := rawSession(t, sessionProtocolClient(t, cfg))
	requireSessionReply(t, ch, "exec", nil, false)
	requireSessionReply(t, ch, "exec", nil, true)
	requireSessionReply(t, ch, "exec", nil, false)
	if calls.Load() != 2 {
		t.Fatalf("handler called %d times", calls.Load())
	}
}

func TestSessionBuiltinExecFreezesLaunchSettings(t *testing.T) {
	observed := make(chan string, 1)
	cfg := &Config{Session: true, NoGlobalEnv: true, ExecHandler: func(s *Session, _ string) (bool, error) {
		observed <- strings.Join(s.Env, "\n")
		<-s.Done()
		return true, nil
	}}
	ch := rawSession(t, sessionProtocolClient(t, cfg))
	requireSessionReply(t, ch, "env", ssh.Marshal(struct{ Name, Value string }{"LAUNCH_TEST", "before"}), true)
	requireSessionReply(t, ch, "exec", ssh.Marshal(struct{ Command string }{"virtual"}), true)
	for range 20 {
		requireSessionReply(t, ch, "env", ssh.Marshal(struct{ Name, Value string }{"LAUNCH_TEST", "after"}), false)
		requireSessionReply(t, ch, "pty-req", ssh.Marshal(ptyRequestPayload{Term: "xterm", Columns: 80, Rows: 24}), false)
		requireSessionReply(t, ch, "exec", ssh.Marshal(struct{ Command string }{"duplicate"}), false)
	}
	select {
	case env := <-observed:
		if !strings.Contains(env, "LAUNCH_TEST=before") || strings.Contains(env, "LAUNCH_TEST=after") {
			t.Fatalf("launch environment changed: %q", env)
		}
	case <-time.After(time.Second):
		t.Fatal("exec did not start")
	}
}
