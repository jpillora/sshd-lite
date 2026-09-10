//go:build !windows

package xssh

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestStartTerminalCommandHonorsNoClientEnv(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "blocked"}[blocked], func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "env")
			terminal, err := StartTerminalCommand(
				&Config{NoClientEnv: blocked, NoInheritEnv: true, NoGlobalEnv: true},
				&net.TCPAddr{}, &net.TCPAddr{}, "xterm", 80, 24,
				[]string{"/bin/sh", "-c", `printf %s "${CLIENT_VALUE-unset}" > "$1"`, "sh", output},
				[]string{"CLIENT_VALUE=present"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if code := terminal.Wait(); code != 0 {
				t.Fatalf("terminal exit code = %d", code)
			}
			terminal.Release()
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			want := "present"
			if blocked {
				want = "unset"
			}
			if string(data) != want {
				t.Fatalf("client environment = %q, want %q", data, want)
			}
		})
	}
}
