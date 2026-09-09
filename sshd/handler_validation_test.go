package sshd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

func TestNewServerRejectsBuiltinHandlerConflicts(t *testing.T) {
	tests := []struct {
		name      string
		category  string
		handler   string
		configure func(*Config)
	}{
		{
			name: "global tcpip-forward", category: "global request", handler: xssh.TCPIPForwardRequestType,
			configure: func(c *Config) {
				c.TCPForwarding = true
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{xssh.TCPIPForwardRequestType: nil}
			},
		},
		{
			name: "global cancel-tcpip-forward", category: "global request", handler: xssh.CancelTCPIPForwardRequestType,
			configure: func(c *Config) {
				c.TCPForwarding = true
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{xssh.CancelTCPIPForwardRequestType: nil}
			},
		},
		{
			name: "session channel", category: "channel", handler: xssh.SessionChannelType,
			configure: func(c *Config) {
				c.ChannelHandlers = map[string]ChannelHandler{xssh.SessionChannelType: nil}
			},
		},
		{
			name: "direct-tcpip channel", category: "channel", handler: xssh.DirectTCPIPChannelType,
			configure: func(c *Config) {
				c.TCPForwarding = true
				c.ChannelHandlers = map[string]ChannelHandler{xssh.DirectTCPIPChannelType: nil}
			},
		},
		{
			name: "pty-req", category: "session request", handler: xssh.PTYRequestType,
			configure: func(c *Config) {
				c.SessionRequestHandlers = map[string]SessionRequestHandler{xssh.PTYRequestType: nil}
			},
		},
		{
			name: "window-change", category: "session request", handler: xssh.WindowChangeRequestType,
			configure: func(c *Config) {
				c.SessionRequestHandlers = map[string]SessionRequestHandler{xssh.WindowChangeRequestType: nil}
			},
		},
		{
			name: "env", category: "session request", handler: xssh.EnvRequestType,
			configure: func(c *Config) {
				c.SessionRequestHandlers = map[string]SessionRequestHandler{xssh.EnvRequestType: nil}
			},
		},
		{
			name: "shell", category: "session request", handler: xssh.ShellRequestType,
			configure: func(c *Config) {
				c.SessionRequestHandlers = map[string]SessionRequestHandler{xssh.ShellRequestType: nil}
			},
		},
		{
			name: "exec", category: "session request", handler: xssh.ExecRequestType,
			configure: func(c *Config) {
				c.SessionRequestHandlers = map[string]SessionRequestHandler{xssh.ExecRequestType: nil}
			},
		},
		{
			name: "sftp", category: "subsystem", handler: xssh.SFTPSubsystem,
			configure: func(c *Config) {
				c.SFTP = true
				c.SubsystemHandlers = map[string]SubsystemHandler{xssh.SFTPSubsystem: nil}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{}
			tt.configure(&c)

			_, err := NewServer(c)
			want := "invalid configuration: custom " + tt.category + " handler \"" + tt.handler + "\" conflicts with built-in handler"
			if err == nil {
				t.Fatalf("NewServer accepted built-in %s handler %q", tt.category, tt.handler)
			}
			if got := err.Error(); got != want {
				t.Fatalf("NewServer error = %q, want %q", got, want)
			}
		})
	}
}

func TestNewServerValidatesHandlersBeforeSetup(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find current executable: %v", err)
	}
	const want = "invalid configuration: custom channel handler \"session\" conflicts with built-in handler"
	tests := []struct {
		name string
		c    Config
	}{
		{
			name: "invalid shell",
			c: Config{
				Shell: filepath.Join(t.TempDir(), "missing-shell"),
				ChannelHandlers: map[string]ChannelHandler{
					xssh.SessionChannelType: nil,
				},
			},
		},
		{
			name: "missing key file",
			c: Config{
				Shell:   executable,
				KeyFile: filepath.Join(t.TempDir(), "missing-key"),
				ChannelHandlers: map[string]ChannelHandler{
					xssh.SessionChannelType: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewServer(tt.c)
			if err == nil || err.Error() != want {
				t.Fatalf("NewServer error = %v, want %q", err, want)
			}
		})
	}
}

func TestNewServerAllowsFeatureGatedHandlerNamesWhenDisabled(t *testing.T) {
	global := func(xssh.Conn, *xssh.Request) error { return nil }
	channel := func(xssh.Conn, ssh.NewChannel) error { return nil }
	subsystem := func(*xssh.Session, *xssh.Request) error { return nil }

	tests := []struct {
		name      string
		configure func(*Config)
		assert    func(*testing.T, *Server)
	}{
		{
			name: "tcpip-forward",
			configure: func(c *Config) {
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{xssh.TCPIPForwardRequestType: global}
			},
			assert: func(t *testing.T, s *Server) {
				requireSameHandler(t, s.xsshConfig.GlobalRequestHandlers[xssh.TCPIPForwardRequestType], global)
			},
		},
		{
			name: "cancel-tcpip-forward",
			configure: func(c *Config) {
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{xssh.CancelTCPIPForwardRequestType: global}
			},
			assert: func(t *testing.T, s *Server) {
				requireSameHandler(t, s.xsshConfig.GlobalRequestHandlers[xssh.CancelTCPIPForwardRequestType], global)
			},
		},
		{
			name: "direct-tcpip",
			configure: func(c *Config) {
				c.ChannelHandlers = map[string]ChannelHandler{xssh.DirectTCPIPChannelType: channel}
			},
			assert: func(t *testing.T, s *Server) {
				requireSameHandler(t, s.xsshConfig.ChannelHandlers[xssh.DirectTCPIPChannelType], channel)
			},
		},
		{
			name: "sftp",
			configure: func(c *Config) {
				c.SubsystemHandlers = map[string]SubsystemHandler{xssh.SFTPSubsystem: subsystem}
			},
			assert: func(t *testing.T, s *Server) {
				requireSameHandler(t, s.xsshConfig.SubsystemHandlers[xssh.SFTPSubsystem], subsystem)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validHandlerTestConfig()
			tt.configure(&c)
			s, err := NewServer(c)
			if err != nil {
				t.Fatalf("NewServer rejected disabled feature handler: %v", err)
			}
			tt.assert(t, s)
			if _, err := xssh.NewConnChecked(nil, nil, nil, s.xsshConfig); err != nil {
				t.Fatalf("accepted server config failed xssh validation: %v", err)
			}
		})
	}
}

func TestNewServerHandlerConflictPrecedenceIsDeterministic(t *testing.T) {
	tests := []struct {
		name string
		c    Config
		want string
	}{
		{
			name: "category order and global name order",
			c: Config{
				TCPForwarding: true,
				SFTP:          true,
				GlobalRequestHandlers: map[string]GlobalRequestHandler{
					xssh.CancelTCPIPForwardRequestType: nil,
					xssh.TCPIPForwardRequestType:       nil,
				},
				ChannelHandlers: map[string]ChannelHandler{xssh.SessionChannelType: nil},
				SessionRequestHandlers: map[string]SessionRequestHandler{
					xssh.PTYRequestType: nil,
				},
				SubsystemHandlers: map[string]SubsystemHandler{xssh.SFTPSubsystem: nil},
			},
			want: "invalid configuration: custom global request handler \"tcpip-forward\" conflicts with built-in handler",
		},
		{
			name: "channel name order",
			c: Config{
				TCPForwarding: true,
				ChannelHandlers: map[string]ChannelHandler{
					xssh.DirectTCPIPChannelType: nil,
					xssh.SessionChannelType:     nil,
				},
			},
			want: "invalid configuration: custom channel handler \"session\" conflicts with built-in handler",
		},
		{
			name: "session request name order",
			c: Config{
				SessionRequestHandlers: map[string]SessionRequestHandler{
					xssh.ExecRequestType: nil,
					xssh.PTYRequestType:  nil,
				},
			},
			want: "invalid configuration: custom session request handler \"pty-req\" conflicts with built-in handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 100; i++ {
				_, err := NewServer(tt.c)
				if err == nil || err.Error() != tt.want {
					t.Fatalf("iteration %d: NewServer error = %v, want %q", i, err, tt.want)
				}
			}
		})
	}
}

func TestNewServerCopiesCustomHandlersWithoutMutatingCallerMaps(t *testing.T) {
	global := func(xssh.Conn, *xssh.Request) error { return nil }
	channel := func(xssh.Conn, ssh.NewChannel) error { return nil }
	session := func(*xssh.Session, *xssh.Request) error { return nil }
	subsystem := func(*xssh.Session, *xssh.Request) error { return nil }
	c := validHandlerTestConfig()
	c.GlobalRequestHandlers = map[string]GlobalRequestHandler{"custom-global": global}
	c.ChannelHandlers = map[string]ChannelHandler{"custom-channel": channel}
	c.SessionRequestHandlers = map[string]SessionRequestHandler{"custom-session": session}
	c.SubsystemHandlers = map[string]SubsystemHandler{"custom-subsystem": subsystem}

	s, err := NewServer(c)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if _, err := xssh.NewConnChecked(nil, nil, nil, s.xsshConfig); err != nil {
		t.Fatalf("accepted server config failed xssh validation: %v", err)
	}
	if len(c.GlobalRequestHandlers) != 1 || len(c.ChannelHandlers) != 1 ||
		len(c.SessionRequestHandlers) != 1 || len(c.SubsystemHandlers) != 1 {
		t.Fatalf("NewServer mutated caller maps: global=%d channel=%d session=%d subsystem=%d",
			len(c.GlobalRequestHandlers), len(c.ChannelHandlers),
			len(c.SessionRequestHandlers), len(c.SubsystemHandlers))
	}
	requireSameHandler(t, c.GlobalRequestHandlers["custom-global"], global)
	requireSameHandler(t, c.ChannelHandlers["custom-channel"], channel)
	requireSameHandler(t, c.SessionRequestHandlers["custom-session"], session)
	requireSameHandler(t, c.SubsystemHandlers["custom-subsystem"], subsystem)

	delete(c.GlobalRequestHandlers, "custom-global")
	delete(c.ChannelHandlers, "custom-channel")
	delete(c.SessionRequestHandlers, "custom-session")
	delete(c.SubsystemHandlers, "custom-subsystem")
	requireSameHandler(t, s.xsshConfig.GlobalRequestHandlers["custom-global"], global)
	requireSameHandler(t, s.xsshConfig.ChannelHandlers["custom-channel"], channel)
	requireSameHandler(t, s.xsshConfig.SessionRequestHandlers["custom-session"], session)
	requireSameHandler(t, s.xsshConfig.SubsystemHandlers["custom-subsystem"], subsystem)
}

func TestDisabledFeatureCustomHandlersAreInvoked(t *testing.T) {
	called := map[string]chan struct{}{
		xssh.TCPIPForwardRequestType:       make(chan struct{}, 1),
		xssh.CancelTCPIPForwardRequestType: make(chan struct{}, 1),
		xssh.DirectTCPIPChannelType:        make(chan struct{}, 1),
		xssh.SFTPSubsystem:                 make(chan struct{}, 1),
	}
	c := validHandlerTestConfig()
	c.GlobalRequestHandlers = map[string]GlobalRequestHandler{
		xssh.TCPIPForwardRequestType: func(_ xssh.Conn, _ *xssh.Request) error {
			called[xssh.TCPIPForwardRequestType] <- struct{}{}
			return nil
		},
		xssh.CancelTCPIPForwardRequestType: func(_ xssh.Conn, _ *xssh.Request) error {
			called[xssh.CancelTCPIPForwardRequestType] <- struct{}{}
			return nil
		},
	}
	c.ChannelHandlers = map[string]ChannelHandler{
		xssh.DirectTCPIPChannelType: func(_ xssh.Conn, ch ssh.NewChannel) error {
			called[xssh.DirectTCPIPChannelType] <- struct{}{}
			return ch.Reject(ssh.Prohibited, "custom handler invoked")
		},
	}
	c.SubsystemHandlers = map[string]SubsystemHandler{
		xssh.SFTPSubsystem: func(_ *xssh.Session, _ *xssh.Request) error {
			called[xssh.SFTPSubsystem] <- struct{}{}
			return nil
		},
	}
	s, err := NewServer(c)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.StartWithContext(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
	})

	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User:            "handler-test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for _, name := range []string{xssh.TCPIPForwardRequestType, xssh.CancelTCPIPForwardRequestType} {
		ok, _, err := client.SendRequest(name, true, nil)
		if err != nil {
			t.Fatalf("send %s request: %v", name, err)
		}
		if !ok {
			t.Fatalf("custom %s handler rejected request", name)
		}
		awaitHandlerCall(t, called[name], name)
	}

	if channel, _, err := client.OpenChannel(xssh.DirectTCPIPChannelType, nil); err == nil {
		_ = channel.Close()
		t.Fatal("custom direct-tcpip handler unexpectedly accepted channel")
	}
	awaitHandlerCall(t, called[xssh.DirectTCPIPChannelType], xssh.DirectTCPIPChannelType)

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()
	if err := session.RequestSubsystem(xssh.SFTPSubsystem); err != nil {
		t.Fatalf("request custom sftp subsystem: %v", err)
	}
	awaitHandlerCall(t, called[xssh.SFTPSubsystem], xssh.SFTPSubsystem)
}

func awaitHandlerCall(t *testing.T, called <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatalf("custom %s handler was not invoked", name)
	}
}

func validHandlerTestConfig() Config {
	return Config{
		AuthType:  "none",
		KeySeed:   "handler-validation-test",
		KeySeedEC: true,
		LogQuiet:  true,
	}
}

func requireSameHandler(t *testing.T, got, want any) {
	t.Helper()
	if got == nil || want == nil || reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("handler was not preserved: got %v, want %v", got, want)
	}
}
