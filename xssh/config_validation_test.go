package xssh

import (
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestValidateConfigRejectsEveryBuiltinCollision(t *testing.T) {
	tests := []struct {
		name      string
		category  string
		handler   string
		configure func(*Config)
	}{
		{
			name: "tcpip-forward", category: "global request", handler: TCPIPForwardRequestType,
			configure: func(c *Config) {
				c.RemoteForwarding = true
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{TCPIPForwardRequestType: nil}
			},
		},
		{
			name: "cancel-tcpip-forward", category: "global request", handler: CancelTCPIPForwardRequestType,
			configure: func(c *Config) {
				c.RemoteForwarding = true
				c.GlobalRequestHandlers = map[string]GlobalRequestHandler{CancelTCPIPForwardRequestType: nil}
			},
		},
		{
			name: "direct-tcpip", category: "channel", handler: DirectTCPIPChannelType,
			configure: func(c *Config) {
				c.LocalForwarding = true
				c.ChannelHandlers = map[string]ChannelHandler{DirectTCPIPChannelType: nil}
			},
		},
		{
			name: "pty-req", category: "session request", handler: PTYRequestType,
			configure: func(c *Config) {
				c.Session = true
				c.SessionRequestHandlers = map[string]SessionRequestHandler{PTYRequestType: nil}
			},
		},
		{
			name: "window-change", category: "session request", handler: WindowChangeRequestType,
			configure: func(c *Config) {
				c.Session = true
				c.SessionRequestHandlers = map[string]SessionRequestHandler{WindowChangeRequestType: nil}
			},
		},
		{
			name: "env", category: "session request", handler: EnvRequestType,
			configure: func(c *Config) {
				c.Session = true
				c.SessionRequestHandlers = map[string]SessionRequestHandler{EnvRequestType: nil}
			},
		},
		{
			name: "shell", category: "session request", handler: ShellRequestType,
			configure: func(c *Config) {
				c.Session = true
				c.SessionRequestHandlers = map[string]SessionRequestHandler{ShellRequestType: nil}
			},
		},
		{
			name: "exec", category: "session request", handler: ExecRequestType,
			configure: func(c *Config) {
				c.Session = true
				c.SessionRequestHandlers = map[string]SessionRequestHandler{ExecRequestType: nil}
			},
		},
		{
			name: "sftp", category: "subsystem", handler: SFTPSubsystem,
			configure: func(c *Config) {
				c.SFTP = true
				c.SubsystemHandlers = map[string]SubsystemHandler{SFTPSubsystem: nil}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{}
			tt.configure(config)
			want := "invalid configuration: custom " + tt.category + " handler \"" + tt.handler + "\" conflicts with built-in handler"

			if err := ValidateConfig(config); err == nil || err.Error() != want {
				t.Fatalf("ValidateConfig error = %v, want %q", err, want)
			}
			conn, err := NewConnChecked(nil, nil, nil, config)
			if conn != nil {
				t.Fatal("NewConnChecked returned a connection after a handler conflict")
			}
			if err == nil || err.Error() != want {
				t.Fatalf("NewConnChecked error = %v, want %q", err, want)
			}

			wantPanic := "xssh.NewConn: " + want + "; use NewConnChecked to handle invalid configuration"
			if got := captureNewConnPanic(config); got != wantPanic {
				t.Fatalf("NewConn panic = %v, want %q", got, wantPanic)
			}
		})
	}
}

func TestValidateConfigAllowsDisabledBuiltinNamesAndPreservesHandlers(t *testing.T) {
	global := func(Conn, *Request) error { return nil }
	channel := func(Conn, ssh.NewChannel) error { return nil }
	session := func(*Session, *Request) error { return nil }
	subsystem := func(*Session, *Request) error { return nil }
	config := &Config{
		GlobalRequestHandlers: map[string]GlobalRequestHandler{
			TCPIPForwardRequestType:       global,
			CancelTCPIPForwardRequestType: global,
		},
		ChannelHandlers: map[string]ChannelHandler{DirectTCPIPChannelType: channel},
		SessionRequestHandlers: map[string]SessionRequestHandler{
			PTYRequestType:          session,
			WindowChangeRequestType: session,
			EnvRequestType:          session,
			ShellRequestType:        session,
			ExecRequestType:         session,
		},
		SubsystemHandlers: map[string]SubsystemHandler{SFTPSubsystem: subsystem},
	}

	if err := ValidateConfig(config); err != nil {
		t.Fatalf("ValidateConfig rejected disabled built-in names: %v", err)
	}
	conn, err := NewConnChecked(nil, nil, nil, config)
	if err != nil {
		t.Fatalf("NewConnChecked rejected disabled built-in names: %v", err)
	}
	xc := conn.(*xconn)
	if len(xc.globalRequestHandlers) != 2 || len(xc.channelHandlers) != 1 ||
		len(xc.sessionRequestHandlers) != 5 || len(xc.subsystemHandlers) != 1 {
		t.Fatal("NewConnChecked did not preserve every disabled-feature handler")
	}
	for name, got := range xc.globalRequestHandlers {
		requireXSSHHandler(t, got, config.GlobalRequestHandlers[name])
	}
	requireXSSHHandler(t, xc.channelHandlers[DirectTCPIPChannelType], channel)
	for name, got := range xc.sessionRequestHandlers {
		requireXSSHHandler(t, got, config.SessionRequestHandlers[name])
	}
	requireXSSHHandler(t, xc.subsystemHandlers[SFTPSubsystem], subsystem)

	if len(config.GlobalRequestHandlers) != 2 || len(config.ChannelHandlers) != 1 ||
		len(config.SessionRequestHandlers) != 5 || len(config.SubsystemHandlers) != 1 {
		t.Fatal("NewConnChecked mutated caller handler maps")
	}
}

func TestNewConnCheckedPreservesUnrelatedHandlersWithBuiltinsEnabled(t *testing.T) {
	global := func(Conn, *Request) error { return nil }
	channel := func(Conn, ssh.NewChannel) error { return nil }
	session := func(*Session, *Request) error { return nil }
	subsystem := func(*Session, *Request) error { return nil }
	config := &Config{
		Session:          true,
		SFTP:             true,
		LocalForwarding:  true,
		RemoteForwarding: true,
		GlobalRequestHandlers: map[string]GlobalRequestHandler{
			"custom-global": global,
		},
		ChannelHandlers: map[string]ChannelHandler{
			"custom-channel": channel,
		},
		SessionRequestHandlers: map[string]SessionRequestHandler{
			"custom-session": session,
		},
		SubsystemHandlers: map[string]SubsystemHandler{
			"custom-subsystem": subsystem,
		},
	}

	conn, err := NewConnChecked(nil, nil, nil, config)
	if err != nil {
		t.Fatalf("NewConnChecked rejected unrelated handlers: %v", err)
	}
	xc := conn.(*xconn)
	requireXSSHHandler(t, xc.globalRequestHandlers["custom-global"], global)
	requireXSSHHandler(t, xc.channelHandlers["custom-channel"], channel)
	requireXSSHHandler(t, xc.sessionRequestHandlers["custom-session"], session)
	requireXSSHHandler(t, xc.subsystemHandlers["custom-subsystem"], subsystem)
	if len(xc.globalRequestHandlers) != 3 || len(xc.channelHandlers) != 2 ||
		len(xc.sessionRequestHandlers) != 6 || len(xc.subsystemHandlers) != 2 {
		t.Fatal("NewConnChecked did not install built-ins alongside custom handlers")
	}
}

func TestValidateConfigDeterministicPrecedence(t *testing.T) {
	config := &Config{
		Session:          true,
		SFTP:             true,
		LocalForwarding:  true,
		RemoteForwarding: true,
		GlobalRequestHandlers: map[string]GlobalRequestHandler{
			CancelTCPIPForwardRequestType: nil,
			TCPIPForwardRequestType:       nil,
		},
		ChannelHandlers: map[string]ChannelHandler{DirectTCPIPChannelType: nil},
		SessionRequestHandlers: map[string]SessionRequestHandler{
			ExecRequestType: nil,
			PTYRequestType:  nil,
		},
		SubsystemHandlers: map[string]SubsystemHandler{SFTPSubsystem: nil},
	}
	const want = "invalid configuration: custom global request handler \"tcpip-forward\" conflicts with built-in handler"
	for i := 0; i < 100; i++ {
		if err := ValidateConfig(config); err == nil || err.Error() != want {
			t.Fatalf("iteration %d: ValidateConfig error = %v, want %q", i, err, want)
		}
	}
}

func TestValidateConfigAndNewConnCheckedAcceptNilConfig(t *testing.T) {
	if err := ValidateConfig(nil); err != nil {
		t.Fatalf("ValidateConfig(nil): %v", err)
	}
	conn, err := NewConnChecked(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewConnChecked nil config: %v", err)
	}
	if conn == nil {
		t.Fatal("NewConnChecked returned nil connection for nil config")
	}
}

func captureNewConnPanic(config *Config) (recovered any) {
	defer func() { recovered = recover() }()
	NewConn(nil, nil, nil, config)
	return nil
}

func requireXSSHHandler(t *testing.T, got, want any) {
	t.Helper()
	if got == nil || want == nil || reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("handler was not preserved: got %v, want %v", got, want)
	}
}
