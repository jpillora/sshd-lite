package client

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestResolveConfigNative(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "conf.d"), 0700)
	p := filepath.Join(d, "config")
	text := "Host work !other\n HostName example.test\n User from-config\n Port 2222\n IdentityFile ~/.ssh/work\n Include conf.d/*.conf\nHost *\n User fallback\n Port 22\n HashKnownHosts yes\n SendEnv LANG LC_*\n"
	text = strings.Replace(text, "Include conf.d/*.conf", "Include "+strconv.Quote(filepath.ToSlash(filepath.Join(d, "conf.d", "*.conf"))), 1)
	os.WriteFile(p, []byte(text), 0600)
	os.WriteFile(filepath.Join(d, "conf.d", "a.conf"), []byte("IdentitiesOnly yes\nIdentityAgent none\n"), 0600)
	o := DialOptions{Host: "work", Sources: Sources{Home: d, Directory: d, LocalUser: "local", ConfigFiles: []string{p}, Environment: map[string]string{"LANG": "C", "LC_TIME": "C", "SECRET": "hidden"}}}
	got, err := Resolve(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "example.test" || got.User != "from-config" || got.Port != "2222" || !got.resolved.only || got.Sources.AgentSocket != "" || !got.resolved.hash {
		t.Fatalf("resolution=%+v state=%+v", got, got.resolved)
	}
	if !reflect.DeepEqual(got.IdentityFiles, []string{filepath.Join(d, ".ssh/work")}) || len(got.SendEnvironment) != 2 {
		t.Fatalf("identities=%v environment=%v", got.IdentityFiles, got.SendEnvironment)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal("native ssh is required for this control")
	}
	out, err := exec.Command(ssh, "-G", "-F", p, "work").Output()
	if err != nil {
		t.Fatal(err)
	}
	nativeOutput := strings.ReplaceAll(string(out), "\r\n", "\n")
	for _, line := range []string{"hostname " + got.Host, "user " + got.User, "port " + got.Port, "identitiesonly yes"} {
		if !strings.Contains(nativeOutput, line+"\n") {
			t.Fatalf("native lacks %q", line)
		}
	}
	o.User = "explicit"
	o.Port = "2200"
	got, err = Resolve(context.Background(), o)
	if err != nil || got.User != "explicit" || got.Port != "2200" {
		t.Fatalf("explicit precedence: %+v %v", got, err)
	}
}
func TestResolveConfigRefusals(t *testing.T) {
	for _, value := range []string{"ProxyJump gateway", "ProxyCommand exec ssh", "StrictHostKeyChecking no", "ServerAliveInterval 10", "Match exec true", "Include */nested.conf"} {
		t.Run(value, func(t *testing.T) {
			d := t.TempDir()
			p := filepath.Join(d, "config")
			os.WriteFile(p, []byte(value+"\n"), 0600)
			_, err := Resolve(context.Background(), DialOptions{Host: "example", User: "git", Sources: Sources{Directory: d, ConfigFiles: []string{p}}})
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("got %v", err)
			}
		})
	}
	d := t.TempDir()
	p := filepath.Join(d, "config")
	os.WriteFile(p, []byte("Include "+strconv.Quote(filepath.ToSlash(p))+"\n"), 0600)
	if _, err := Resolve(context.Background(), DialOptions{Host: "example", User: "git", Sources: Sources{Directory: d, ConfigFiles: []string{p}}}); err == nil {
		t.Fatal("include cycle accepted")
	}
}
func TestResolveDoesNotInheritProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "unexpected")
	got, err := Resolve(context.Background(), DialOptions{Host: "example", User: "git"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sources.AgentSocket != "" || len(got.IdentityFiles) != 0 || got.Sources.Home != "" {
		t.Fatal("inherited process authority")
	}
	if runtime.GOOS == "windows" && len(got.resolved.known) != 0 {
		t.Fatal("inherited Unix system trust path on Windows")
	}
}

func TestResolveStockSendEnv351(t *testing.T) {
	for _, tc := range []struct {
		name        string
		config      string
		environment map[string]string
		want        []EnvironmentEntry
		unsupported string
		invalid     bool
	}{
		{
			name:        "absent",
			environment: map[string]string{"LANG": "C", "LC_TIME": "C", "UNLISTED": "sentinel"},
			want:        []EnvironmentEntry{{"LANG", "C"}, {"LC_TIME", "C"}},
		},
		{
			name:        "present",
			environment: map[string]string{"LANG": "C", "LC_TIME": "C", "COLORTERM": "truecolor", "NO_COLOR": "1", "UNLISTED": "sentinel"},
			want:        []EnvironmentEntry{{"COLORTERM", "truecolor"}, {"LANG", "C"}, {"LC_TIME", "C"}, {"NO_COLOR", "1"}},
		},
		{
			name:        "present empty",
			environment: map[string]string{"COLORTERM": "", "NO_COLOR": ""},
			want:        []EnvironmentEntry{{"COLORTERM", ""}, {"NO_COLOR", ""}},
		},
		{
			name:        "minus patterns",
			config:      "SendEnv -COLORTERM -NO_COLOR\n",
			environment: map[string]string{"LANG": "C", "COLORTERM": "truecolor", "NO_COLOR": "1"},
			want:        []EnvironmentEntry{{"LANG", "C"}},
		},
		{name: "unlisted directive operand", config: "SendEnv UNLISTED\n", unsupported: "SendEnv"},
		{name: "wildcard operand", config: "SendEnv *\n", unsupported: "SendEnv"},
		{name: "color wildcard", config: "SendEnv COLORTERM*\n", unsupported: "SendEnv"},
		{name: "case sensitive", config: "SendEnv no_color\n", unsupported: "SendEnv"},
		{name: "unknown directive", config: "UnknownOption351 yes\n", unsupported: "unknownoption351"},
		{name: "control byte", environment: map[string]string{"NO_COLOR": "ignored\nline"}, invalid: true},
		{name: "long value", environment: map[string]string{"COLORTERM": strings.Repeat("x", 4097)}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config")
			config := "Host *\n SendEnv LANG LC_* COLORTERM NO_COLOR\n" + tc.config
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Resolve(context.Background(), DialOptions{
				Host: "example.test",
				User: "git",
				Sources: Sources{
					Directory:   dir,
					ConfigFiles: []string{path},
					Environment: tc.environment,
				},
			})
			if tc.unsupported != "" {
				var unsupported *UnsupportedError
				if !errors.As(err, &unsupported) || unsupported.Directive != tc.unsupported {
					t.Fatalf("wanted unsupported %s, got %v", tc.unsupported, err)
				}
				return
			}
			if tc.invalid {
				if err == nil || err.Error() != "ssh client: invalid session environment" {
					t.Fatalf("wanted bounded environment refusal, got %v", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got.SendEnvironment, tc.want) {
				t.Fatalf("selected environment = %v, want %v, err = %v", got.SendEnvironment, tc.want, err)
			}
		})
	}
}
