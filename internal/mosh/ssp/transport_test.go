package ssp

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	wire "github.com/unixshells/mosh-go"
)

func transports(t *testing.T) (*Transport, *Transport) {
	t.Helper()
	key, _, err := wire.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := wire.NewOCB(key)
	b, _ := wire.NewOCB(key)
	return NewTransport(a, false), NewTransport(b, true)
}
func transfer(from, to *Transport) *Update {
	from.ForceNextSend()
	var update *Update
	for _, dg := range from.Tick() {
		if u, _ := to.RecvUpdate(dg); u != nil {
			update = u
		}
	}
	return update
}
func TestReorderedFragmentsAndReplay(t *testing.T) {
	a, b := transports(t)
	payload := make([]byte, 16000)
	rand.Read(payload)
	a.SetPending(payload)
	packets := a.Tick()
	if len(packets) < 3 {
		t.Fatal("expected fragmentation")
	}
	var u *Update
	for i := len(packets) - 1; i >= 0; i-- {
		if next, _ := b.RecvUpdate(packets[i]); next != nil {
			u = next
		}
	}
	if u == nil || !bytes.Equal(u.Diff, payload) {
		t.Fatal("reordered fragments did not reassemble")
	}
	last := b.LastRecv()
	for _, dg := range packets {
		if update, fresh := b.RecvUpdate(dg); update != nil || fresh {
			t.Fatal("replayed state")
		}
	}
	if !b.LastRecv().Equal(last) {
		t.Fatal("replay renewed idle timer")
	}
	transfer(b, a)
	if a.AckedByRemote() != 1 {
		t.Fatal("missing ACK")
	}
}

func TestFreshnessDoesNotDependOnClockAdvancing(t *testing.T) {
	a, b := transports(t)
	// Model a coarse clock returning a value no later than the transport's
	// previous timestamp. Windows can return equal values for adjacent calls;
	// placing the old value in the future makes the regression deterministic.
	previous := time.Now().Add(time.Hour)
	b.mu.Lock()
	b.lastRecv = previous
	b.mu.Unlock()
	a.SetPending([]byte("input"))
	packets := a.Tick()
	if len(packets) != 1 {
		t.Fatalf("initial state used %d datagrams, want 1", len(packets))
	}
	update, fresh := b.RecvUpdate(packets[0])
	if update == nil || !fresh {
		t.Fatal("authenticated new state was not reported as fresh")
	}
	if b.LastRecv().After(previous) {
		t.Fatal("test did not exercise a non-advancing receive timestamp")
	}
}
func TestLongSessionStatePruningAndShutdown(t *testing.T) {
	a, b := transports(t)
	for i := 1; i <= 1500; i++ {
		a.SetPending([]byte(fmt.Sprint(i)))
		u := transfer(a, b)
		if u == nil || u.NewNum != uint64(i) {
			t.Fatalf("missing state %d", i)
		}
		transfer(b, a)
	}
	if len(b.receivedNums) > 2 {
		t.Fatalf("retained %d states", len(b.receivedNums))
	}
	a.StartShutdown()
	u := transfer(a, b)
	if u == nil || len(u.Diff) != 0 || !b.RemoteShutdown() {
		t.Fatal("empty shutdown state lost")
	}
	// Drop the first acknowledgement, then retransmit the shutdown.
	b.ForceNextSend()
	b.Tick()
	transfer(a, b)
	transfer(b, a)
	if !a.ShutdownAcked() {
		t.Fatal("shutdown did not recover from lost acknowledgement")
	}
}

func TestTimestampEchoExcludesTimeHeld(t *testing.T) {
	a, b := transports(t)
	for _, age := range []time.Duration{40 * time.Millisecond, 2 * time.Second} {
		a.lastTS = 1234
		a.lastTSAt = time.Now().Add(-age)
		a.ForceNextSend()
		packet := a.Tick()[0]
		var nonce [12]byte
		copy(nonce[4:], packet[:8])
		plain := b.ocb.Decrypt(nonce[:], packet[8:])
		reply := binary.BigEndian.Uint16(plain[2:4])
		if age >= time.Second {
			if reply != 0xffff {
				t.Fatalf("stale timestamp echoed: %d", reply)
			}
		} else if reply < 1274 || reply > 1294 {
			t.Fatalf("timestamp did not exclude residence time: %d", reply)
		}
	}
}
