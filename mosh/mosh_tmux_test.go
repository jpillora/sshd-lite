//go:build linux

package mosh

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/term"
)

// tmuxTerminal uses tmux itself as the receiving terminal emulator. This is
// independent of the Go emulator used by the implementation and other tests.
// All sockets are private; the user's tmux server is never addressed.
type tmuxTerminal struct {
	t              *testing.T
	binary, socket string
}

func (p *tmuxTerminal) command(args ...string) string {
	p.t.Helper()
	cmd := exec.Command(p.binary, append([]string{"-S", p.socket}, args...)...)
	cmd.Env = append(os.Environ(), "TMUX=", "TERM=xterm-256color", "LC_ALL=C.UTF-8")
	b, err := cmd.CombinedOutput()
	if err != nil {
		p.t.Fatalf("tmux %v: %v: %s", args, err, b)
	}
	return string(b)
}
func (p *tmuxTerminal) text() string     { return p.command("capture-pane", "-p", "-t", "local:0.0") }
func (p *tmuxTerminal) literal(s string) { p.command("send-keys", "-l", "-t", "local:0.0", s) }
func (p *tmuxTerminal) keys(keys ...string) {
	p.command(append([]string{"send-keys", "-t", "local:0.0"}, keys...)...)
}
func (p *tmuxTerminal) line(s string) { p.literal(s); p.keys("Enter") }
func (p *tmuxTerminal) contains(s string) {
	p.t.Helper()
	waitInterop(p.t, func() bool { return strings.Contains(p.text(), s) }, p.text)
}
func newTmuxTerminal(t *testing.T, dir string) *tmuxTerminal {
	p := &tmuxTerminal{t: t, binary: referenceBinary(t, "tmux"), socket: filepath.Join(dir, "outer.sock")}
	p.command("-f", "/dev/null", "new-session", "-d", "-s", "local", "-x", "100", "-y", "30", "/bin/sh")
	t.Cleanup(func() { exec.Command(p.binary, "-S", p.socket, "kill-server").Run() })
	p.command("set-option", "-t", "local", "status", "off")
	return p
}
func liteMoshArguments(t *testing.T, c Config) []string {
	t.Helper()
	binary := os.Getenv("MOSH_TEST_LITE")
	if binary == "" {
		if os.Getenv("MOSH_TEST_REQUIRED") != "" {
			t.Fatal("MOSH_TEST_LITE is required")
		}
		t.Skip("set MOSH_TEST_LITE to the built CLI")
	}
	_, addr, _ := c.target()
	_, port, _ := net.SplitHostPort(addr)
	args := []string{binary, "client", "--mosh", "--port", port, "--known-hosts", c.KnownHosts}
	if c.Identity != "" {
		args = append(args, "--identity", c.Identity)
	}
	if c.Password != "" {
		args = append(args, "--password", c.Password)
	}
	if c.MoshServer != "" {
		args = append(args, "--mosh-server", c.MoshServer)
	}
	return append(args, "--", c.Destination)
}

func TestMoshInActualTmuxTerminals(t *testing.T) {
	for _, pair := range []string{"debian-client-lite-server", "lite-client-debian-server", "lite-client-lite-server"} {
		t.Run(pair, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "mosh-tmux-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			var c Config
			if pair == "lite-client-debian-server" {
				c = startOpenSSH(t)
			} else {
				c, _ = startServer(t, true)
			}
			outer := newTmuxTerminal(t, dir)
			remoteSocket := filepath.Join(dir, "remote.sock")
			tmux := referenceBinary(t, "tmux")
			remote := &tmuxTerminal{t: t, binary: tmux, socket: remoteSocket}
			t.Cleanup(func() { exec.Command(tmux, "-S", remoteSocket, "kill-server").Run() })
			var clientArgs []string
			if pair == "debian-client-lite-server" {
				referenceBinary(t, "sshpass")
				_, addr, _ := c.target()
				_, port, _ := net.SplitHostPort(addr)
				clientArgs = []string{referenceBinary(t, "mosh"), "--ssh=sshpass -p pass ssh -p " + port + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", "--", c.Destination[:strings.LastIndex(c.Destination, ":")]}
			} else {
				clientArgs = liteMoshArguments(t, c)
			}
			launch := func(remoteArgs []string, label string) {
				t.Helper()
				args := append(append([]string{}, clientArgs...), remoteArgs...)
				var words []string
				for _, arg := range args {
					words = append(words, shellQuote(arg))
				}
				before, after := filepath.Join(dir, label+"-before"), filepath.Join(dir, label+"-after")
				prefix := ""
				if bin := os.Getenv("MOSH_TEST_BIN"); bin != "" {
					prefix = "PATH=" + shellQuote(bin+":"+os.Getenv("PATH")) + " "
				}
				outer.line("printf '\\nPRESERVED_" + label + "\\n'; stty -g > " + shellQuote(before) + "; " + prefix + "TERM=xterm-256color MOSH_PREDICTION_DISPLAY=never " + strings.Join(words, " ") + "; code=$?; stty -g > " + shellQuote(after) + "; printf '\\nLOCAL_" + label + "_%s\\n' \"$code\"")
			}
			restored := func(label string) {
				t.Helper()
				outer.contains("LOCAL_" + label + "_0")
				waitInterop(t, func() bool {
					for _, line := range strings.Split(outer.text(), "\n") {
						if strings.TrimSpace(line) == "PRESERVED_"+label {
							return true
						}
					}
					return false
				}, outer.text)
				before, e1 := os.ReadFile(filepath.Join(dir, label+"-before"))
				after, e2 := os.ReadFile(filepath.Join(dir, label+"-after"))
				if e1 != nil || e2 != nil || string(before) != string(after) {
					t.Fatalf("terminal modes not restored: %q => %q (%v, %v)", before, after, e1, e2)
				}
			}
			launch([]string{tmux, "-S", remoteSocket, "-f", "/dev/null", "new-session", "-s", "remote", "/bin/bash --noprofile --norc"}, "DETACH")
			marker := filepath.Join(dir, "tty")
			outer.line("test -t 0 && test -t 1 && printf '%s' \"$TMUX\" > " + shellQuote(marker) + "; printf 'REMOTE_%s\\n' READY")
			waitInterop(t, func() bool {
				b, _ := os.ReadFile(marker)
				return strings.HasPrefix(string(b), remoteSocket+",") && strings.Contains(outer.text(), "REMOTE_READY")
			}, outer.text)
			t.Log("remote shell has actual TTYs and is inside tmux")
			// Readline left-arrow editing: the shell must execute EDIT_PASS, not XASS.
			outer.literal("printf 'EDIT_%s\\n' XASS")
			outer.keys("Left", "Left", "Left", "Left", "Delete")
			outer.literal("P")
			outer.keys("Enter")
			outer.contains("EDIT_PASS")
			outer.line("printf '\\033[31mCOLOR_%s\\033[0m 界 é\\n' RED")
			outer.contains("COLOR_RED 界 é")
			colored := outer.command("capture-pane", "-p", "-e", "-t", "local:0.0")
			if !strings.Contains(colored, "\x1b[31mCOLOR_RED") {
				t.Fatalf("red rendition missing: %q", colored)
			}
			// A real split and pane switch, driven through the Mosh keyboard path.
			outer.keys("C-b", "%")
			outer.line("printf 'SECOND_%s\\n' PANE")
			outer.contains("SECOND_PANE")
			outer.keys("C-b", "o")
			outer.line("printf 'FIRST_%s\\n' PANE")
			outer.contains("FIRST_PANE")
			// Close the other pane so dimensions below are unambiguous.
			remote.command("kill-pane", "-t", "remote:0.1")
			for _, size := range [][2]int{{73, 19}, {120, 42}, {100, 30}} {
				outer.command("resize-window", "-t", "local:0", "-x", fmt.Sprint(size[0]), "-y", fmt.Sprint(size[1]))
				expected := fmt.Sprintf("%d %d", size[1]-1, size[0])
				waitInterop(t, func() bool {
					b, _ := exec.Command(tmux, "-S", remoteSocket, "display-message", "-p", "-t", "remote:0.0", "#{pane_tty}").Output()
					// tmux updates layout metadata before applying its debounced PTY resize.
					actual, err := exec.Command("stty", "-F", strings.TrimSpace(string(b)), "size").Output()
					return err == nil && strings.TrimSpace(string(actual)) == expected
				}, outer.text)
				outer.line("stty size > " + shellQuote(filepath.Join(dir, "size")) + "; cat " + shellQuote(filepath.Join(dir, "size")))
				waitInterop(t, func() bool { return strings.Contains(outer.text(), expected) }, func() string {
					b, _ := os.ReadFile(filepath.Join(dir, "size"))
					remote, _ := exec.Command(tmux, "-S", remoteSocket, "capture-pane", "-p", "-t", "remote:0.0").Output()
					return fmt.Sprintf("expected=%s stty=%s remote=%s local=%s", expected, b, remote, outer.text())
				})
			}
			t.Log("readline arrows, colored Unicode, split panes, switching, and three resizes passed")
			// History belongs to remote tmux: exercise its full-screen copy-mode redraw.
			remote.command("set-window-option", "-t", "remote:0", "mode-keys", "vi")
			outer.line("for ((i=0;i<65;i++)); do printf 'HISTORY_%03d\\n' \"$i\"; done")
			outer.contains("HISTORY_064")
			outer.keys("C-b", "[")
			waitInterop(t, func() bool {
				b, _ := exec.Command(tmux, "-S", remoteSocket, "display-message", "-p", "-t", "remote:0.0", "#{pane_in_mode}").Output()
				return strings.TrimSpace(string(b)) == "1"
			}, outer.text)
			outer.keys("g")
			outer.contains("HISTORY_000")
			outer.keys("q")
			waitInterop(t, func() bool {
				b, _ := exec.Command(tmux, "-S", remoteSocket, "display-message", "-p", "-t", "remote:0.0", "#{pane_in_mode}").Output()
				return strings.TrimSpace(string(b)) == "0"
			}, outer.text)
			outer.keys("C-b", "d")
			restored("DETACH")
			launch([]string{tmux, "-S", remoteSocket, "attach-session", "-t", "remote"}, "EXIT")
			outer.contains("HISTORY_064")
			outer.line("printf 'REATTACHED_%s\\n' OK")
			outer.contains("REATTACHED_OK")
			outer.line("exit")
			restored("EXIT")
			outer.line("printf 'LOCAL_%s\\n' USABLE")
			outer.contains("LOCAL_USABLE")
			t.Log("copy mode, detach/reattach, normal exit, local screen and exact termios restoration passed")
			launch([]string{tmux, "-S", remoteSocket, "new-session", "-s", "escape", "/bin/bash --noprofile --norc"}, "ESCAPE")
			outer.line("printf 'ESCAPE_%s\\n' READY")
			outer.contains("ESCAPE_READY")
			outer.literal("\x1e.")
			restored("ESCAPE")
			t.Log("Ctrl-^ . disconnect and terminal restoration passed")
			probe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			keyFile := filepath.Join(dir, "keys")
			launch([]string{probe, "-test.run=^TestMoshTerminalKeyProbe$", "--", "mosh-key-probe", keyFile}, "KEYS")
			outer.contains("PROBE_APPLICATION")
			outer.keys("Up")
			outer.contains("PROBE_NORMAL")
			outer.keys("Up")
			restored("KEYS")
			got, err := os.ReadFile(keyFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "\x1bOA\x1b[A" {
				t.Fatalf("wrong application/normal arrow bytes: %q", got)
			}
			t.Log("application and normal cursor-key encodings passed")

		})
	}
}

// Run by the remote command path as a raw-terminal program. Reading literal
// bytes distinguishes cursor modes even when readline accepts both encodings.
func TestMoshTerminalKeyProbe(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "mosh-key-probe" {
		return
	}
	before, err := term.MakeRaw(0)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(0, before)
	var keys []byte
	for _, mode := range []string{"\x1b[?1hPROBE_APPLICATION", "\x1b[?1lPROBE_NORMAL"} {
		fmt.Print(mode)
		b := make([]byte, 3)
		if _, err := io.ReadFull(os.Stdin, b); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, b...)
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], keys, 0600); err != nil {
		t.Fatal(err)
	}
}
