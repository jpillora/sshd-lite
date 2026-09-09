//go:build linux

package mosh

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	vt "github.com/charmbracelet/x/vt"
	"github.com/jpillora/sshd-lite/internal/mosh"
)

// udpFaultProxy drops every fifth packet, reverses occasional pairs, and changes
// its source port midway through the test. Both peers see ordinary UDP sockets.
func udpFaultProxy(t *testing.T, target *net.UDPAddr) *net.UDPAddr {
	t.Helper()
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var peer *net.UDPAddr
	var sockets []*net.UDPConn
	var wg sync.WaitGroup
	forward := func(back *net.UDPConn) {
		defer wg.Done()
		buf := make([]byte, 65536)
		count := 0
		var held []byte
		for {
			n, err := back.Read(buf)
			if err != nil {
				return
			}
			count++
			if count%5 == 0 {
				continue
			}
			mu.Lock()
			addr := peer
			mu.Unlock()
			if addr == nil {
				continue
			}
			data := append([]byte(nil), buf[:n]...)
			if count%7 == 0 {
				held = data
				continue
			}
			front.WriteToUDP(data, addr)
			if held != nil {
				front.WriteToUDP(held, addr)
				held = nil
			}
		}
	}
	newSocket := func() *net.UDPConn {
		c, err := net.DialUDP("udp", nil, target)
		if err != nil {
			t.Fatal(err)
		}
		sockets = append(sockets, c)
		wg.Add(1)
		go forward(c)
		return c
	}
	back := newSocket()
	roamed := newSocket()
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 65536)
		count := 0
		var held []byte
		started := time.Now()
		for {
			n, addr, err := front.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			peer = addr
			mu.Unlock()
			count++
			if time.Since(started) > time.Second {
				back = roamed
			}
			if count%5 == 0 {
				continue
			}
			data := append([]byte(nil), buf[:n]...)
			if count%7 == 0 {
				held = data
				continue
			}
			back.Write(data)
			if held != nil {
				back.Write(held)
				held = nil
			}
		}
	}()
	t.Cleanup(func() {
		front.Close()
		for _, c := range sockets {
			c.Close()
		}
		wg.Wait()
	})
	return front.LocalAddr().(*net.UDPAddr)
}

type renderedOutput struct {
	mu     sync.Mutex
	screen *vt.Emulator
}

func (r *renderedOutput) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.screen.Write(b)
}
func (r *renderedOutput) text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.screen.String() }

func TestDebianMoshLossReorderRoaming(t *testing.T) {
	for _, side := range []string{"debian-client", "lite-client"} {
		t.Run(side, func(t *testing.T) {
			var config Config
			if side == "debian-client" {
				referenceBinary(t, "mosh-client")
				config, _ = startServer(t, true)
			} else {
				config = startOpenSSH(t)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			sshConn, err := config.connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			creds, err := bootstrapMosh(ctx, sshConn, mosh.Request{Cols: 100, Rows: 30}, config.MoshServer, config.Command)
			sshConn.Close()
			if err != nil {
				t.Fatal(err)
			}
			proxy := udpFaultProxy(t, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: creds.Port})
			var send func(string)
			var screen func() string
			var done <-chan error
			if side == "debian-client" {
				cmd := exec.Command(referenceBinary(t, "mosh-client"), proxy.IP.String(), fmt.Sprint(proxy.Port))
				cmd.Env = []string{"MOSH_KEY=" + creds.Key}
				terminal := startReferencePTY(t, cmd)
				send = func(s string) { terminal.send(t, s) }
				screen = terminal.text
				done = terminal.done
			} else {
				conn, err := net.DialUDP("udp", nil, proxy)
				if err != nil {
					t.Fatal(err)
				}
				input := make(chan []byte, 32)
				sizes := make(chan mosh.Request, 1)
				sizes <- mosh.Request{Cols: 100, Rows: 30}
				out := &renderedOutput{screen: vt.NewEmulator(100, 30)}
				out.screen.InputPipe().(interface{ Close() error }).Close()
				result := make(chan error, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					_, err := mosh.RunClient(ctx, conn, creds.Key, mosh.Request{Cols: 100, Rows: 30}, input, sizes, out)
					result <- err
				}()
				t.Cleanup(func() {
					cancel()
					conn.Close()
					<-finished
					out.mu.Lock()
					out.screen.Close()
					out.mu.Unlock()
				})
				send = func(s string) { input <- []byte(s) }
				screen = out.text
				done = result
			}
			marker := filepath.Join(t.TempDir(), "input")
			send("stty -echo\n")
			for i := range 20 {
				send(fmt.Sprintf("printf '%02d\\n' >> '%s'\n", i, marker))
				time.Sleep(65 * time.Millisecond)
			}
			// Force a large, poorly compressible screen update, then verify convergence.
			send("head -c 1600 /dev/urandom | base64; printf '\\nCONVERGED_%s\\n' SCREEN\n")
			waitInterop(t, func() bool {
				b, _ := os.ReadFile(marker)
				return len(strings.Fields(string(b))) == 20 && strings.Contains(screen(), "CONVERGED_SCREEN")
			}, func() string {
				b, _ := os.ReadFile(marker)
				select {
				case err := <-done:
					return fmt.Sprintf("client ended: %v; file=%q screen=%s", err, b, screen())
				default:
				}
				return fmt.Sprintf("file=%q screen=%s", b, screen())
			})
			b, _ := os.ReadFile(marker)
			var want strings.Builder
			for i := range 20 {
				fmt.Fprintf(&want, "%02d\n", i)
			}
			if string(b) != want.String() {
				t.Fatalf("repeated or missing input: %q", b)
			}
			send("exit\n")
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("shutdown under loss timed out")
			}
		})
	}
}
