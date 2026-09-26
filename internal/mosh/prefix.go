package mosh

import (
	"bytes"
	"encoding/binary"
)

const RoutingPrefixHeaderSize = 8

// A client-to-server Mosh nonce always has its high direction bit clear, so an
// 0x80 marker cannot be mistaken for a valid unprefixed inbound datagram.
var routingPrefixMagic = [4]byte{0x80, 'M', 'P', 0x01}

// AddRoutingPrefix prepends the optional, cleartext routing envelope used by
// sshd-lite clients. A zero prefix disables the extension.
func AddRoutingPrefix(prefix uint32, datagram []byte) []byte {
	if prefix == 0 {
		return datagram
	}
	prefixed := make([]byte, RoutingPrefixHeaderSize+len(datagram))
	copy(prefixed, routingPrefixMagic[:])
	binary.BigEndian.PutUint32(prefixed[4:], prefix)
	copy(prefixed[RoutingPrefixHeaderSize:], datagram)
	return prefixed
}

// stripRoutingPrefix removes a recognized routing envelope. The envelope is
// deliberately outside Mosh encryption so a load balancer can consume it.
func stripRoutingPrefix(datagram []byte) ([]byte, uint32, bool) {
	if len(datagram) < RoutingPrefixHeaderSize || !bytes.Equal(datagram[:4], routingPrefixMagic[:]) {
		return datagram, 0, false
	}
	return datagram[RoutingPrefixHeaderSize:], binary.BigEndian.Uint32(datagram[4:8]), true
}
