package mosh

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

// A terminal can write queries without consuming its stdin. Close must release
// its pending I/O, as closing an actual PTY does.
type queryTerminal struct {
	output               *bytes.Reader
	writing, done        chan struct{}
	writeOnce, closeOnce sync.Once
}

func (t *queryTerminal) Read(b []byte) (int, error) {
	if t.output.Len() > 0 {
		return t.output.Read(b)
	}
	<-t.done
	return 0, io.EOF
}
func (t *queryTerminal) Write(b []byte) (int, error) {
	t.writeOnce.Do(func() { close(t.writing) })
	<-t.done
	return 0, io.ErrClosedPipe
}
func (t *queryTerminal) Resize(uint16, uint16) error { return nil }
func (t *queryTerminal) Wait() int                   { <-t.done; return 0 }
func (t *queryTerminal) Close() error                { t.closeOnce.Do(func() { close(t.done) }); return nil }

func TestQueryBackpressureDoesNotBlockShutdown(t *testing.T) {
	s, err := Listen(context.Background(), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	terminal := &queryTerminal{output: bytes.NewReader(bytes.Repeat([]byte("\x1b[6n"), 500)), writing: make(chan struct{}), done: make(chan struct{})}
	stopped := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { go func() { s.Close(); close(stopped) }() }) }
	t.Cleanup(func() {
		terminal.Close()
		stop()
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Error("server shutdown remained blocked")
		}
	})
	creds, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return terminal, nil })
	if err != nil {
		t.Fatal(err)
	}
	ocb, err := wire.NewOCBFromBase64(creds.Key)
	if err != nil {
		t.Fatal(err)
	}
	tr := ssp.NewTransport(ocb, false)
	conn := udpClient(t, s)
	tr.ForceNextSend()
	for _, packet := range tr.Tick() {
		conn.Write(packet)
	}
	select {
	case <-terminal.writing:
	case <-time.After(time.Second):
		t.Fatal("no terminal response was generated")
	}
	// The bounded response queue must end the overloaded session by itself;
	// cancellation and expiry cannot depend on a blocked emulator write returning.
	select {
	case <-terminal.done:
	case <-time.After(time.Second):
		t.Fatal("terminal query backpressure stalled the session")
	}
	stop()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("query backpressure blocked server shutdown")
	}
}
