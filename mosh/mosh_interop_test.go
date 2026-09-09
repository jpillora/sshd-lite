//go:build linux

package mosh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	vt "github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func referenceBinary(t *testing.T, name string) string {
	t.Helper()
	if dir := os.Getenv("MOSH_TEST_BIN"); dir != "" && strings.HasPrefix(name, "mosh") {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path, err := exec.LookPath(name)
	if err != nil {
		if os.Getenv("MOSH_TEST_REQUIRED") != "" {
			t.Fatal(name + " is not installed")
		}
		t.Skip(name + " is not installed")
	}
	return path
}

type referencePTY struct {
	file     *os.File
	cmd      *exec.Cmd
	done     chan error
	readDone chan struct{}
	mu       sync.Mutex
	output   strings.Builder
	screen   *vt.Emulator
}

func startReferencePTY(t *testing.T, cmd *exec.Cmd) *referencePTY {
	t.Helper()
	cmd.Env = append(append(os.Environ(), cmd.Env...), "TERM=xterm-256color", "LC_ALL=C.UTF-8", "MOSH_PREDICTION_DISPLAY=never")
	if dir := os.Getenv("MOSH_TEST_BIN"); dir != "" {
		cmd.Env = append(cmd.Env, "PATH="+dir+":"+os.Getenv("PATH"))
	}
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	p := &referencePTY{file: file, cmd: cmd, done: make(chan error, 1), readDone: make(chan struct{}), screen: vt.NewEmulator(100, 30)}
	p.screen.InputPipe().(io.Closer).Close()
	go func() {
		defer close(p.readDone)
		b := make([]byte, 8192)
		for {
			n, err := file.Read(b)
			if n > 0 {
				p.mu.Lock()
				p.output.Write(b[:n])
				p.screen.Write(b[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill(); file.Close(); <-p.readDone; p.screen.Close() })
	return p
}
func (p *referencePTY) text() string { p.mu.Lock(); defer p.mu.Unlock(); return p.screen.String() }
func (p *referencePTY) raw() string  { p.mu.Lock(); defer p.mu.Unlock(); return p.output.String() }
func (p *referencePTY) send(t *testing.T, text string) {
	t.Helper()
	if _, err := io.WriteString(p.file, text); err != nil {
		t.Fatal(err)
	}
}
func waitInterop(t *testing.T, f func() bool, describe func() string) {
	t.Helper()
	end := time.Now().Add(12 * time.Second)
	for !f() {
		if time.Now().After(end) {
			t.Fatal(describe())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDebianMoshClientAgainstLiteServer(t *testing.T) {
	launcher := referenceBinary(t, "mosh")
	referenceBinary(t, "mosh-client")
	referenceBinary(t, "sshpass")
	config, dir := startServer(t, true)
	_, addr, _ := config.target()
	port := strings.Split(addr, ":")[1]
	sshCommand := fmt.Sprintf("sshpass -p pass ssh -p %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", port)
	for _, remoteIP := range []string{"proxy", "remote"} {
		t.Run(remoteIP, func(t *testing.T) {
			cmd := exec.Command(launcher, "--ssh="+sshCommand, "--experimental-remote-ip="+remoteIP, "user@127.0.0.1")
			terminal := startReferencePTY(t, cmd)
			marker := filepath.Join(dir, "executed-"+remoteIP)
			terminal.send(t, fmt.Sprintf("printf 'EXECUTED_%%s\\n' DEBIAN > '%s'; printf 'SCREEN_%%s\\n' DEBIAN\n", marker))
			waitInterop(t, func() bool {
				b, _ := os.ReadFile(marker)
				return string(b) == "EXECUTED_DEBIAN\n" && strings.Contains(terminal.text(), "SCREEN_DEBIAN")
			}, func() string { return "Debian client failed: " + terminal.raw() })
			if err := pty.Setsize(terminal.file, &pty.Winsize{Cols: 91, Rows: 37}); err != nil {
				t.Fatal(err)
			}
			terminal.mu.Lock()
			terminal.screen.Resize(91, 37)
			terminal.mu.Unlock()
			terminal.send(t, "sleep 0.2; stty size\n")
			waitInterop(t, func() bool { return strings.Contains(terminal.text(), "37 91") }, terminal.raw)
			terminal.send(t, "exit\n")
			select {
			case err := <-terminal.done:
				if err != nil {
					t.Fatalf("Debian client exit: %v: %s", err, terminal.raw())
				}
			case <-time.After(8 * time.Second):
				t.Fatal("Debian client did not complete standard shutdown: " + terminal.raw())
			}
			if strings.Contains(terminal.raw(), "Fatal assertion") {
				t.Fatal(terminal.raw())
			}
		})
	}
}

func TestLiteClientAgainstReferenceMoshServer(t *testing.T) {
	referenceBinary(t, "mosh-server")
	// The SSH transport executes the actual installed mosh-server. No virtual
	// handler or native key request participates in this pairing.
	config := startOpenSSH(t)
	code, out, _, err := runInput(t, config, "printf 'REFERENCE_%s\\n' SERVER\nexit 7\n")
	if err != nil || code != 0 || !strings.Contains(out, "REFERENCE_SERVER") {
		t.Fatalf("code=%d err=%v output=%q", code, err, out)
	}
}

func TestLiteMoshCLIAgainstDebian(t *testing.T) {
	binary := os.Getenv("MOSH_TEST_LITE")
	if binary == "" {
		if os.Getenv("MOSH_TEST_REQUIRED") != "" {
			t.Fatal("MOSH_TEST_LITE is required")
		}
		t.Skip("set MOSH_TEST_LITE to a built sshd-lite executable")
	}
	c := startOpenSSH(t)
	_, addr, _ := c.target()
	_, port, _ := net.SplitHostPort(addr)
	cmd := exec.Command(binary, "client", "--mosh", "--port", port, "--identity", c.Identity, "--known-hosts", c.KnownHosts, "--mosh-server", c.MoshServer, c.Destination)
	terminal := startReferencePTY(t, cmd)
	terminal.send(t, "printf 'CLI_%s\\n' DEBIAN\n")
	waitInterop(t, func() bool { return strings.Contains(terminal.text(), "CLI_DEBIAN") }, terminal.raw)
	if err := pty.Setsize(terminal.file, &pty.Winsize{Cols: 91, Rows: 37}); err != nil {
		t.Fatal(err)
	}
	terminal.mu.Lock()
	terminal.screen.Resize(91, 37)
	terminal.mu.Unlock()
	terminal.send(t, "sleep 0.2; stty size\n")
	waitInterop(t, func() bool { return strings.Contains(terminal.text(), "37 91") }, terminal.raw)
	terminal.send(t, "exit\n")
	select {
	case err := <-terminal.done:
		if err != nil {
			t.Fatalf("CLI exit: %v: %s", err, terminal.raw())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("CLI shutdown timed out")
	}
}

func TestDebianMoshFullscreenAndEscape(t *testing.T) {
	launcher := referenceBinary(t, "mosh")
	tmux := referenceBinary(t, "tmux")
	referenceBinary(t, "sshpass")
	c, dir := startServer(t, true)
	_, addr, _ := c.target()
	_, port, _ := net.SplitHostPort(addr)
	cmd := exec.Command(launcher, "--ssh=sshpass -p pass ssh -p "+port+" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", "user@127.0.0.1")
	terminal := startReferencePTY(t, cmd)
	socket := filepath.Join(dir, "tmux.sock")
	marker := filepath.Join(dir, "tmux-active")
	t.Cleanup(func() { exec.Command(tmux, "-S", socket, "kill-server").Run() })
	terminal.send(t, fmt.Sprintf("%s -S '%s' -f /dev/null new-session -s interop\n", tmux, socket))
	// This executes inside the full-screen application, not in the outer shell.
	terminal.send(t, fmt.Sprintf("printf '%%s' \"$TMUX\" > '%s'; printf 'FULLSCREEN_%%s\\n' DEBIAN\n", marker))
	waitInterop(t, func() bool {
		b, _ := os.ReadFile(marker)
		return strings.HasPrefix(string(b), socket+",") && strings.Contains(terminal.text(), "FULLSCREEN_DEBIAN")
	}, terminal.raw)
	terminal.send(t, "\x02d")
	terminal.send(t, "\x1e.")
	select {
	case err := <-terminal.done:
		if err != nil {
			t.Fatalf("escape exit: %v: %s", err, terminal.raw())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("escape did not disconnect")
	}
	if strings.Contains(terminal.raw(), "Fatal assertion") {
		t.Fatal(terminal.raw())
	}
}

func TestDebianClientAgainstLiteServerCLI(t *testing.T) {
	binary := os.Getenv("MOSH_TEST_LITE")
	if binary == "" {
		if os.Getenv("MOSH_TEST_REQUIRED") != "" {
			t.Fatal("MOSH_TEST_LITE is required")
		}
		t.Skip("set MOSH_TEST_LITE to a built sshd-lite executable")
	}
	launcher := referenceBinary(t, "mosh")
	referenceBinary(t, "sshpass")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	l.Close()
	dir := t.TempDir()
	log, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	daemon := exec.Command(binary, "--mosh", "--host", "127.0.0.1", "--port", port, "--shell", "/bin/sh", "--workdir", dir, "--keyseed", "mosh-cli-test", "--keyseed-ec", "user:pass")
	daemon.Stdout = log
	daemon.Stderr = log
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Process.Kill(); daemon.Wait(); log.Close() })
	waitInterop(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
		return false
	}, func() string { b, _ := os.ReadFile(log.Name()); return string(b) })
	terminal := startReferencePTY(t, exec.Command(launcher, "--ssh=sshpass -p pass ssh -p "+port+" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", "user@127.0.0.1"))
	marker := filepath.Join(dir, "cli-executed")
	terminal.send(t, fmt.Sprintf("printf 'SERVER_%%s\\n' CLI > '%s'; printf 'SERVER_%%s\\n' CLI\n", marker))
	waitInterop(t, func() bool {
		b, _ := os.ReadFile(marker)
		return string(b) == "SERVER_CLI\n" && strings.Contains(terminal.text(), "SERVER_CLI")
	}, terminal.raw)
	terminal.send(t, "exit\n")
	select {
	case err := <-terminal.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("server CLI shutdown timed out")
	}
}

func TestMoshLiteralCommandArguments(t *testing.T) {
	for _, peer := range []string{"lite", "debian"} {
		t.Run(peer, func(t *testing.T) {
			var c Config
			if peer == "lite" {
				c, _ = startServer(t, true)
			} else {
				c = startOpenSSH(t)
			}
			c.Command = []string{"printf", "LITERAL_%s_END\\n", "it's $HOME `whoami`"}
			code, out, _, err := runInput(t, c, "")
			screen := vt.NewEmulator(80, 24)
			screen.InputPipe().(io.Closer).Close()
			screen.Write([]byte(out))
			rendered := screen.String()
			screen.Close()
			if err != nil || code != 0 || !strings.Contains(rendered, "LITERAL_it's $HOME `whoami`_END") {
				t.Fatalf("literal command: code=%d err=%v out=%q", code, err, out)
			}
		})
	}
}

func TestPublicMoshSessionAgainstDebian(t *testing.T) {
	c := startOpenSSH(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := c.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	session, err := Start(ctx, conn, ClientConfig{Server: c.MoshServer, Columns: 91, Rows: 37, Command: []string{"/bin/sh", "-c", "read line; printf 'LIBRARY_%s\\n' \"$line\"; exit 7"}, Output: &output})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := io.WriteString(session, "DEBIAN\n"); err != nil {
		t.Fatal(err)
	}
	code, err := session.Wait()
	if err != nil || code != 0 || !strings.Contains(output.String(), "LIBRARY_DEBIAN") {
		t.Fatalf("library session with Debian: code=%d err=%v output=%q", code, err, output.String())
	}
}
