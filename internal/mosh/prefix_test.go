package mosh

import (
	"bytes"
	"testing"
)

func TestRoutingPrefixEnvelope(t *testing.T) {
	datagram := []byte{0x01, 0x02, 0x03}
	if got := AddRoutingPrefix(0, datagram); &got[0] != &datagram[0] {
		t.Fatal("zero prefix did not leave datagram untouched")
	}
	prefixed := AddRoutingPrefix(0x01020304, datagram)
	want := []byte{0x80, 'M', 'P', 0x01, 0x01, 0x02, 0x03, 0x04, 0x01, 0x02, 0x03}
	if !bytes.Equal(prefixed, want) {
		t.Fatalf("prefixed datagram = %x, want %x", prefixed, want)
	}
	stripped, prefix, ok := stripRoutingPrefix(prefixed)
	if !ok || prefix != 0x01020304 || !bytes.Equal(stripped, datagram) {
		t.Fatalf("strip = %x, %#x, %v", stripped, prefix, ok)
	}
	if stripped, prefix, ok := stripRoutingPrefix(datagram); ok || prefix != 0 || !bytes.Equal(stripped, datagram) {
		t.Fatalf("stock datagram changed: %x, %#x, %v", stripped, prefix, ok)
	}
	for _, malformed := range [][]byte{
		{0x80, 'M', 'P'},
		{0x80, 'M', 'P', 0x02, 0, 0, 0, 1},
	} {
		if _, _, ok := stripRoutingPrefix(malformed); ok {
			t.Fatalf("accepted malformed envelope %x", malformed)
		}
	}
}
