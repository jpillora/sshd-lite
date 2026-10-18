//go:build unix

package client

import (
	"context"
	"errors"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTerminalSecretAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "secret", true: "cancel"}[cancelled], func(t *testing.T) {
			master, slave, e := pty.Open()
			if e != nil {
				t.Fatal(e)
			}
			defer master.Close()
			defer slave.Close()
			fd := int(slave.Fd())
			before, e := term.GetState(fd)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type answer struct {
				b []byte
				e error
			}
			done := make(chan answer, 1)
			go func() { b, e := readTerminal(ctx, fd, "Passphrase: ", true); done <- answer{b, e} }()
			wait, c := context.WithTimeout(context.Background(), 2*time.Second)
			defer c()
			var transcript strings.Builder
			buf := make([]byte, 128)
			for !strings.Contains(transcript.String(), "Passphrase: ") {
				if e := terminalReady(wait, int(master.Fd()), unix.POLLIN); e != nil {
					t.Fatal(e)
				}
				n, e := unix.Read(int(master.Fd()), buf)
				if e != nil {
					t.Fatal(e)
				}
				transcript.Write(buf[:n])
			}
			if cancelled {
				cancel()
			} else {
				master.Write([]byte("unlogged-secret\r"))
			}
			select {
			case a := <-done:
				if cancelled {
					if !errors.Is(a.e, context.Canceled) {
						t.Fatal(a.e)
					}
				} else if a.e != nil || string(a.b) != "unlogged-secret" {
					t.Fatalf("answer=%q err=%v", a.b, a.e)
				}
				clear(a.b)
			case <-time.After(2 * time.Second):
				t.Fatal("terminal read did not end")
			}
			after, e := term.GetState(fd)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("terminal was not restored")
			}
			poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
			if n, _ := unix.Poll(poll, 0); n > 0 {
				n, e := unix.Read(int(master.Fd()), buf)
				if e == nil {
					transcript.Write(buf[:n])
				}
			}
			if strings.Contains(transcript.String(), "unlogged-secret") {
				t.Fatal("secret echoed")
			}
		})
	}
}
