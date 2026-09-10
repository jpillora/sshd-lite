package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd"
)

func TestMoshStartupLoggingAndQuiet(t *testing.T) {
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	}()
	log.SetFlags(0)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	c := &serverCommand{Config: sshd.Config{
		Host:      "127.0.0.1",
		Port:      "0",
		AuthType:  "none",
		KeySeed:   "mosh-log-test",
		KeySeedEC: true,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Mosh: true}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, c); err != nil {
		t.Fatal(err)
	}
	if output := logs.String(); !strings.Contains(output, "Mosh enabled") || !strings.Contains(output, "Listening on udp://") || !strings.Contains(output, "for mosh connections") {
		t.Fatalf("Mosh startup logs = %q", output)
	}

	logs.Reset()
	c.LogQuiet = true
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runServer(ctx, c); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("quiet Mosh logs = %q", logs.String())
	}
}

func TestCLIExposesAndParsesClientCommand(t *testing.T) {
	cli, server, client := newCLI()
	parsed, err := cli.ParseArgsError([]string{"sshd-lite", "client", "--accept", "--port", "2222", "user@example.com", "printf", "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if server.Command != "client" || client.Destination != "user@example.com" || client.Port != "2222" || !client.Accept || !reflect.DeepEqual(client.Command, []string{"printf", "ok"}) {
		t.Fatalf("server=%#v client=%#v", server, client)
	}
	if !strings.Contains(parsed.Help(), "client") {
		t.Fatalf("root help does not list client:\n%s", parsed.Help())
	}
}

func TestClientCommandHasVersionFlag(t *testing.T) {
	for _, args := range [][]string{
		{"sshd-lite", "client", "--version"},
		{"/usr/local/bin/ssh-lite", "--version"},
	} {
		cli, _, _ := newCLI()
		parsed, err := cli.ParseArgsError(normalizeCLIArgs(args))
		if err == nil || !strings.Contains(parsed.Help(), version) {
			t.Fatalf("args=%v version result: help=%q err=%v", args, parsed.Help(), err)
		}
	}
}

func TestCLIAliasAndLegacyNoEnvParse(t *testing.T) {
	clientCLI, clientServer, client := newCLI()
	if _, err := clientCLI.ParseArgsError(normalizeCLIArgs([]string{"/usr/local/bin/ssh-lite", "user@example.com"})); err != nil {
		t.Fatal(err)
	}
	if clientServer.Command != "client" || client.Destination != "user@example.com" {
		t.Fatalf("alias server=%#v client=%#v", clientServer, client)
	}

	serverCLI, server, _ := newCLI()
	if _, err := serverCLI.ParseArgsError(normalizeCLIArgs([]string{"sshd-lite", "--noenv", "user:pass"})); err != nil {
		t.Fatal(err)
	}
	if !server.NoClientEnv {
		t.Fatal("legacy --noenv did not set NoClientEnv")
	}
}

func TestCLIKeepsDaemonPositionalAuth(t *testing.T) {
	cli, server, _ := newCLI()
	if _, err := cli.ParseArgsError([]string{"sshd-lite", "user:pass", "--port", "2222"}); err != nil {
		t.Fatal(err)
	}
	if server.Command != "" || server.AuthType != "user:pass" || server.Port != "2222" {
		t.Fatalf("server=%#v", server)
	}
}

func TestNormalizeCLIArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"alias", []string{"/usr/local/bin/ssh-lite", "host"}, []string{"/usr/local/bin/ssh-lite", "client", "host"}},
		{"windows alias", []string{`C:\\bin\\ssh-lite.EXE`, "host"}, []string{`C:\\bin\\ssh-lite.EXE`, "client", "host"}},
		{"legacy env", []string{"sshd-lite", "--noenv", "user:pass"}, []string{"sshd-lite", "--no-client-env", "user:pass"}},
		{"legacy equals", []string{"sshd-lite", "--noenv=false", "user:pass"}, []string{"sshd-lite", "--no-client-env=false", "user:pass"}},
		{"command flag value", []string{"sshd-lite", "--keyseed", "client", "--noenv", "user:pass"}, []string{"sshd-lite", "--keyseed", "client", "--no-client-env", "user:pass"}},
		{"legacy spelling as value", []string{"sshd-lite", "--keyseed", "--noenv", "user:pass"}, []string{"sshd-lite", "--keyseed", "--noenv", "user:pass"}},
		{"remote command untouched", []string{"sshd-lite", "client", "host", "echo", "--noenv"}, []string{"sshd-lite", "client", "host", "echo", "--noenv"}},
		{"double dash untouched", []string{"sshd-lite", "user:pass", "--", "--noenv"}, []string{"sshd-lite", "user:pass", "--", "--noenv"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeCLIArgs(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalize = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
	}{{"", 64}, {"invalid", 64}, {" 17 ", 17}, {"-1", -1}, {"0", 0}} {
		t.Setenv("TEST_INTEGER", tt.value)
		if got := getEnvInt("TEST_INTEGER", 64); got != tt.want {
			t.Fatalf("getEnvInt(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
	t.Setenv("MAX_PENDING_HANDSHAKES", "23")
	_, server, _ := newCLI()
	if server.MaxPendingHandshakes != 23 {
		t.Fatalf("CLI max pending handshakes = %d", server.MaxPendingHandshakes)
	}
}
