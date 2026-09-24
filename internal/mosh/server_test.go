package mosh

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

type echoTerminal struct {
	reader  *io.PipeReader
	writer  *io.PipeWriter
	done    chan struct{}
	once    sync.Once
	resized chan Request
}

func newEcho() *echoTerminal {
	r, w := io.Pipe()
	return &echoTerminal{reader: r, writer: w, done: make(chan struct{}), resized: make(chan Request, 10)}
}
func (e *echoTerminal) Read(b []byte) (int, error)  { return e.reader.Read(b) }
func (e *echoTerminal) Write(b []byte) (int, error) { return e.writer.Write(b) }
func (e *echoTerminal) Resize(c, r uint16) error    { e.resized <- Request{Cols: c, Rows: r}; return nil }
func (e *echoTerminal) Wait() int                   { <-e.done; return 0 }
func (e *echoTerminal) Close() error {
	e.once.Do(func() { close(e.done); e.reader.Close(); e.writer.Close() })
	return nil
}

func testServer(t *testing.T, idle time.Duration) *Server {
	t.Helper()
	s, err := Listen(context.Background(), &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	s.idle = idle
	t.Cleanup(func() { s.Close() })
	return s
}
func issue(t *testing.T, s *Server, e *echoTerminal) (Credentials, *wire.Transport) {
	t.Helper()
	credentials, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return e, nil })
	if err != nil {
		t.Fatal(err)
	}
	ocb, err := wire.NewOCBFromBase64(credentials.Key)
	if err != nil {
		t.Fatal(err)
	}
	return credentials, wire.NewTransport(ocb, false)
}
func udpClient(t *testing.T, s *Server) *net.UDPConn {
	t.Helper()
	c, err := net.DialUDP("udp", nil, s.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func sessionCount(s *Server) int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sessions) }
func send(tr *wire.Transport, c *net.UDPConn) []byte {
	tr.ForceNextSend()
	dg := tr.Tick()[0]
	c.Write(dg)
	return dg
}
func receive(t *testing.T, c *net.UDPConn, tr *wire.Transport) {
	t.Helper()
	b := make([]byte, 65536)
	c.SetReadDeadline(time.Now().Add(time.Second))
	n, err := c.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	tr.Recv(b[:n])
}

func receiveUpdate(t *testing.T, c *net.UDPConn, tr *ssp.Transport) *ssp.Update {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	b := make([]byte, 65536)
	for {
		if err := c.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		n, err := c.Read(b)
		if err != nil {
			t.Fatal(err)
		}
		if update, _ := tr.RecvUpdate(b[:n]); update != nil && len(update.Diff) > 0 {
			return update
		}
	}
}

func hoststring(t *testing.T, update *ssp.Update) string {
	t.Helper()
	instructions, err := wire.UnmarshalHostMessage(update.Diff)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, instruction := range instructions {
		b.Write(instruction.Hoststring)
	}
	return b.String()
}

func inputDatagram(t *testing.T, ocb *wire.OCB, seq, state uint64, diff []byte) []byte {
	t.Helper()
	ti := wire.TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          0,
		NewNum:          state,
		ThrowawayNum:    0,
		Diff:            diff,
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(ti.Marshal()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f := wire.Fragment{ID: state, Final: true, Payload: compressed.Bytes()}
	fragment := f.Marshal()
	plain := make([]byte, 4+len(fragment))
	binary.BigEndian.PutUint16(plain[0:], uint16(time.Now().UnixMilli()))
	binary.BigEndian.PutUint16(plain[2:], 0xffff)
	copy(plain[4:], fragment)
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], seq)
	datagram := make([]byte, 8)
	binary.BigEndian.PutUint64(datagram, seq)
	return append(datagram, ocb.Encrypt(nonce[:], plain)...)
}

func TestUnusedKeyExpiresWithoutStartingShell(t *testing.T) {
	s := testServer(t, 100*time.Millisecond)
	var starts atomic.Int32
	_, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { starts.Add(1); return newEcho(), nil })
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sessionCount(s) == 0 })
	if starts.Load() != 0 {
		t.Fatal("unused key started a shell")
	}
}

func TestEmptyInputStateDoesNotGenerateEchoUpdate(t *testing.T) {
	s := testServer(t, 3*time.Second)
	e := newEcho()
	credentials, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return e, nil })
	if err != nil {
		t.Fatal(err)
	}
	ocb, err := wire.NewOCBFromBase64(credentials.Key)
	if err != nil {
		t.Fatal(err)
	}
	c := udpClient(t, s)
	if _, err := c.Write(inputDatagram(t, ocb, 1, 1, nil)); err != nil {
		t.Fatal(err)
	}
	client := wire.NewTransport(ocb, false)
	deadline := time.Now().Add(150 * time.Millisecond)
	buf := make([]byte, 65536)
	for {
		if err := c.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		n, err := c.Read(buf)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if diff := client.Recv(buf[:n]); len(diff) != 0 {
			t.Fatalf("empty input state generated %d-byte terminal update", len(diff))
		}
	}
}

func TestScreenUpdatesDoNotWaitForAcknowledgement(t *testing.T) {
	s := testServer(t, 3*time.Second)
	e := newEcho()
	credentials, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return e, nil })
	if err != nil {
		t.Fatal(err)
	}
	ocb, err := wire.NewOCBFromBase64(credentials.Key)
	if err != nil {
		t.Fatal(err)
	}
	c := udpClient(t, s)
	if _, err := c.Write(inputDatagram(t, ocb, 1, 1, nil)); err != nil {
		t.Fatal(err)
	}
	client := ssp.NewTransport(ocb, false)
	if _, err := e.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	first := receiveUpdate(t, c, client)
	if text := hoststring(t, first); !strings.Contains(text, "first") {
		t.Fatalf("first update = %q", text)
	}

	// Deliberately send no acknowledgement for first. A mobile peer may batch
	// ACKs for seconds; newer terminal state must still be sent promptly from
	// the last confirmed base.
	if _, err := e.Write([]byte("second")); err != nil {
		t.Fatal(err)
	}
	second := receiveUpdate(t, c, client)
	if second.NewNum <= first.NewNum {
		t.Fatalf("new state %d did not supersede unacknowledged state %d", second.NewNum, first.NewNum)
	}
	if second.OldNum != first.OldNum {
		t.Fatalf("superseding state changed unacknowledged base from %d to %d", first.OldNum, second.OldNum)
	}
	if text := hoststring(t, second); !strings.Contains(text, "firstsecond") {
		t.Fatalf("superseding update = %q", text)
	}
}

func TestKeepaliveReplayAndRoaming(t *testing.T) {
	s := testServer(t, 300*time.Millisecond)
	e := newEcho()
	_, tr := issue(t, s, e)
	c := udpClient(t, s)
	// Keep a session alive for several complete idle periods without keystrokes.
	for range 12 {
		send(tr, c)
		time.Sleep(60 * time.Millisecond)
	}
	if sessionCount(s) != 1 {
		t.Fatal("keepalive did not renew session")
	}
	// Move the authenticated session to a new source port.
	roamed := udpClient(t, s)
	send(tr, roamed)
	receive(t, roamed, tr)
	// Replaying one authenticated packet must not renew the key.
	replay := send(tr, roamed)
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		roamed.Write(replay)
		time.Sleep(40 * time.Millisecond)
	}
	waitFor(t, func() bool { return sessionCount(s) == 0 })
	select {
	case <-e.done:
	default:
		t.Fatal("expired shell was not closed")
	}
	// A fresh packet with the expired key cannot resurrect it.
	send(tr, roamed)
	if sessionCount(s) != 0 {
		t.Fatal("expired key was resurrected")
	}
}

func TestInvalidKeyCannotAssociateOrRenew(t *testing.T) {
	s := testServer(t, 200*time.Millisecond)
	var starts atomic.Int32
	_, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { starts.Add(1); return newEcho(), nil })
	if err != nil {
		t.Fatal(err)
	}
	key, _, _ := wire.GenerateKey()
	ocb, _ := wire.NewOCB(key)
	tr := wire.NewTransport(ocb, false)
	c := udpClient(t, s)
	for range 10 {
		send(tr, c)
		time.Sleep(40 * time.Millisecond)
	}
	if starts.Load() != 0 || sessionCount(s) != 0 {
		t.Fatal("invalid traffic associated or renewed key")
	}
}

func TestSharedPortSessionsAndResize(t *testing.T) {
	s := testServer(t, 3*time.Second)
	a, b := newEcho(), newEcho()
	_, ta := issue(t, s, a)
	_, tb := issue(t, s, b)
	ca, cb := udpClient(t, s), udpClient(t, s)
	ta.SetPending(wire.MarshalUserMessage([]wire.UserInstruction{{Width: 91, Height: 37}}))
	tb.SetPending(wire.MarshalUserMessage([]wire.UserInstruction{{Width: 72, Height: 29}}))
	send(ta, ca)
	send(tb, cb)
	select {
	case r := <-a.resized:
		if r.Cols != 91 || r.Rows != 37 {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("first resize not delivered")
	}
	select {
	case r := <-b.resized:
		if r.Cols != 72 || r.Rows != 29 {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("second resize not delivered")
	}
	s.Close()
	if sessionCount(s) != 0 {
		t.Fatal("shutdown left sessions")
	}
}

func TestSessionLimitAndRevoke(t *testing.T) {
	s := testServer(t, time.Minute)
	var revokes []func()
	for range maxSessions {
		_, revoke, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return newEcho(), nil })
		if err != nil {
			t.Fatal(err)
		}
		revokes = append(revokes, revoke)
	}
	if _, _, err := s.Issue(Request{Cols: 80, Rows: 24}, nil); err == nil {
		t.Fatal("session limit not enforced")
	}
	for _, revoke := range revokes {
		revoke()
	}
	waitFor(t, func() bool { return sessionCount(s) == 0 })
}

// lossyConn drops packets in both directions while preserving ordinary UDP
// socket semantics, exercising retransmission of input, screen updates and exit.
type lossyConn struct {
	net.Conn
	writes, reads int
}

func (c *lossyConn) Write(b []byte) (int, error) {
	c.writes++
	if c.writes%3 == 0 {
		return len(b), nil
	}
	return c.Conn.Write(b)
}
func (c *lossyConn) Read(b []byte) (int, error) {
	for {
		n, err := c.Conn.Read(b)
		if err != nil {
			return n, err
		}
		c.reads++
		if c.reads%4 != 0 {
			return n, nil
		}
	}
}

type recordingTerminal struct {
	*echoTerminal
	mu    sync.Mutex
	input []byte
}

func (r *recordingTerminal) Write(b []byte) (int, error) {
	r.mu.Lock()
	r.input = append(r.input, b...)
	r.mu.Unlock()
	return r.echoTerminal.Write(b)
}
func (r *recordingTerminal) text() string { r.mu.Lock(); defer r.mu.Unlock(); return string(r.input) }

type recordingOutput struct {
	mu   sync.Mutex
	data []byte
}

func (r *recordingOutput) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, b...)
	return len(b), nil
}
func (r *recordingOutput) text() string { r.mu.Lock(); defer r.mu.Unlock(); return string(r.data) }

func TestClientRetransmitsWithoutDuplicatingInput(t *testing.T) {
	s := testServer(t, 5*time.Second)
	terminal := &recordingTerminal{echoTerminal: newEcho()}
	credentials, _, err := s.Issue(Request{Cols: 80, Rows: 24}, func() (Terminal, error) { return terminal, nil })
	if err != nil {
		t.Fatal(err)
	}
	conn := udpClient(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	input := make(chan []byte, 10)
	output := &recordingOutput{}
	done := make(chan error, 1)
	go func() {
		code, err := RunClient(ctx, &lossyConn{Conn: conn}, credentials.Key, Request{Cols: 80, Rows: 24}, input, nil, output)
		if err == nil && code != 0 {
			err = fmt.Errorf("exit %d", code)
		}
		done <- err
	}()
	expected := ""
	for i := range 20 {
		text := fmt.Sprintf("%02d ", i)
		expected += text
		input <- []byte(text)
		time.Sleep(20 * time.Millisecond)
	}
	close(input)
	waitFor(t, func() bool { return terminal.text() == expected })
	waitFor(t, func() bool { return strings.Contains(output.text(), "19") })
	terminal.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("client failed to receive exit over lossy UDP")
	}
	if terminal.text() != expected {
		t.Fatalf("duplicated or missing input: %q", terminal.text())
	}
}
