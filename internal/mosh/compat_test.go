package mosh

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

func TestOverlappingInputBases(t *testing.T) {
	s := newInputStates()
	var got []byte
	apply := func(old, new, drop uint64, text string) {
		t.Helper()
		is, err := s.apply(&ssp.Update{OldNum: old, NewNum: new, ThrowawayNum: drop, Diff: wire.MarshalUserMessage([]wire.UserInstruction{{Keys: []byte(text)}})})
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range is {
			got = append(got, i.Keys...)
		}
	}
	apply(0, 1, 0, "abc")
	apply(0, 3, 0, "abcdef")
	apply(0, 2, 0, "abcd")
	apply(1, 4, 1, "defgh")
	if string(got) != "abcdefgh" {
		t.Fatalf("input duplicated or lost: %q", got)
	}
	if _, ok := s.ends[0]; ok {
		t.Fatal("old input base retained")
	}
}
func TestBootstrapLiteralsAndValidation(t *testing.T) {
	b, ok, err := ParseBootstrap(`mosh-server new -s -c 256 -l 'LANG=C.UTF-8' -- printf 'it'\''s' a\ b "a\"b" '$HOME'`)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if !reflect.DeepEqual(b.Command, []string{"printf", "it's", "a b", `a"b`, "$HOME"}) {
		t.Fatalf("literal argv: %#v", b.Command)
	}
	for _, command := range []string{`echo mosh-server new`, `mosh-server new | cat`, `mosh-server new; touch nope`, `mosh-server new -- $(touch nope)`, `/usr/bin/mosh-server new`} {
		if _, handled, _ := ParseBootstrap(command); handled {
			t.Fatalf("intercepted unrelated/expanded command %q", command)
		}
	}
	b, ok, err = ParseBootstrap("sh -c '" + connectionProbe + "'; mosh-server new -p 2200:2300")
	if !ok || err != nil || !b.PrintConnection {
		t.Fatal("Debian address probe not recognized")
	}
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	if err := b.ValidateListener(2222, addr); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateListener(2400, addr); err == nil {
		t.Fatal("accepted different UDP port")
	}
	for _, arg := range []string{"-p 0", "-p 65536", "-p 2300:2200", "-i 127.0.0.2", "-l PATH=/tmp", "--invalid"} {
		b, _, err := ParseBootstrap("mosh-server new " + arg)
		if err == nil {
			err = b.ValidateListener(2222, addr)
		}
		if err == nil {
			t.Fatalf("accepted %s", arg)
		}
	}
}
func TestShutdownReleasesShellAndRepeatsAck(t *testing.T) {
	s := testServer(t, 10*time.Second)
	e := newEcho()
	credentials, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return e, nil })
	if err != nil {
		t.Fatal(err)
	}
	ocb, _ := wire.NewOCBFromBase64(credentials.Key)
	tr := ssp.NewTransport(ocb, false)
	c := udpClient(t, s)
	tr.SetPending(wire.MarshalUserMessage([]wire.UserInstruction{{Keys: []byte("hello")}}))
	for _, dg := range tr.Tick() {
		c.Write(dg)
	}
	tr.StartShutdown()
	sendShutdown := func() {
		tr.ForceNextSend()
		for _, dg := range tr.Tick() {
			c.Write(dg)
		}
	}
	sendShutdown()
	select {
	case <-e.done:
	case <-time.After(time.Second):
		t.Fatal("shell remained after shutdown")
	}
	// Discard the first response batch and request another shutdown acknowledgement.
	c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 65536)
	for {
		if _, err := c.Read(buf); err != nil {
			break
		}
	}
	sendShutdown()
	c.SetReadDeadline(time.Now().Add(time.Second))
	for !tr.ShutdownAcked() {
		n, err := c.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		tr.RecvUpdate(buf[:n])
	}
}

func TestConcurrentBootstrapArguments(t *testing.T) {
	for worker := range 16 {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			t.Parallel()
			want := fmt.Sprintf("worker %d $literal", worker)
			for range 50 {
				b, handled, err := ParseBootstrap(fmt.Sprintf("mosh-server new -l 'LANG=C.UTF-8' -- printf '%%s' '%s'", want))
				if err != nil || !handled || len(b.Command) != 3 || b.Command[2] != want {
					t.Fatalf("concurrent argument corruption: %#v handled=%v err=%v", b, handled, err)
				}
			}
		})
	}
}
