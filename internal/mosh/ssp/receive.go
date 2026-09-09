// Adapted from mosh-go v0.5.2 (MIT); see ../LICENSE.mosh-go.
package ssp

import (
	"encoding/binary"
	"time"

	wire "github.com/unixshells/mosh-go"
)

func (t *Transport) Recv(data []byte) []byte {
	if update := t.RecvUpdate(data); update != nil {
		return update.Diff
	}
	return nil
}

// RecvUpdate exposes every accepted state, including empty diffs and shutdown.
func (t *Transport) RecvUpdate(data []byte) *Update {
	if len(data) < minDatagram {
		return nil
	}

	dirSeq := binary.BigEndian.Uint64(data[:8])

	// Verify direction.
	if dirSeq&dirToClient != t.toLocal&dirToClient {
		return nil
	}

	seq := dirSeq & SequenceMask

	t.mu.Lock()
	if t.replayed(seq) {
		t.mu.Unlock()
		return nil // replay
	}
	t.mu.Unlock()

	// Decrypt.
	var nonce [12]byte
	copy(nonce[4:], data[:8])
	plaintext := t.ocb.Decrypt(nonce[:], data[8:])
	if plaintext == nil {
		return nil
	}

	// Parse timestamp header (4 bytes).
	if len(plaintext) < 4 {
		return nil
	}
	remoteTS := binary.BigEndian.Uint16(plaintext[0:])
	// plaintext[2:4] is timestamp_reply — used for RTT.
	tsReply := binary.BigEndian.Uint16(plaintext[2:])
	payload := plaintext[4:]

	// Update crypto sequence.
	t.mu.Lock()
	if t.replayed(seq) {
		t.mu.Unlock()
		return nil
	}
	newest := !t.seqInMaxSet || seq > t.seqInMax
	if newest {
		t.seqInMax = seq
	}
	t.seqInMaxSet = true
	t.seqSeen[seq%uint64(len(t.seqSeen))] = seq + 1
	t.lastRecv = time.Now()
	if newest {
		t.lastTS = remoteTS
		t.lastTSAt = t.lastRecv
	}
	t.mu.Unlock()

	// RTT estimation from timestamp echo.
	if newest && tsReply != 0xffff {
		t.updateRTT(tsReply)
	}

	// Parse fragment.
	if len(payload) < fragmentHeaderSize {
		// Heartbeat with no fragment — that's fine.
		return nil
	}
	frag, err := wire.UnmarshalFragment(payload)
	if err != nil {
		return nil
	}

	// Reassemble.
	t.mu.Lock()
	msg := t.assembler.Add(frag)
	t.mu.Unlock()
	if msg == nil {
		return nil
	}

	// Decompress → parse TransportInstruction.
	decompressed := zlibDecompress(msg)
	if decompressed == nil {
		return nil
	}
	var ti wire.TransportInstruction
	if err := ti.Unmarshal(decompressed); err != nil || ti.ProtocolVersion != 2 || ti.ThrowawayNum > ti.OldNum {
		return nil
	}

	if len(ti.LatchCaps) > 0 {
		t.mu.Lock()
		t.remoteCaps = ti.LatchCaps
		t.mu.Unlock()
	}

	// Process SSP fields (matching upstream mosh recv logic).
	t.mu.Lock()
	defer t.mu.Unlock()

	// Process ack from remote.
	if ti.AckNum > t.ackedByRemote && ti.AckNum <= t.sentNum {
		t.ackedByRemote = ti.AckNum
		if t.ackedByRemote >= t.sentNum && t.pendingDiff != nil {
			t.pendingDiff = nil
			t.diffSent = false
			t.hasPendingBase = false
		}
	}

	// Check if we already have new_num (dedup).
	for _, n := range t.receivedNums {
		if n == ti.NewNum {
			return nil
		}
	}

	// Check if we have old_num (required to apply diff).
	hasOld := false
	for _, n := range t.receivedNums {
		if n == ti.OldNum {
			hasOld = true
			break
		}
	}
	if !hasOld {
		return nil
	}

	// Process throwaway.
	if ti.ThrowawayNum > t.throwawayNum {
		t.throwawayNum = ti.ThrowawayNum
		filtered := t.receivedNums[:0]
		for _, n := range t.receivedNums {
			if n >= t.throwawayNum {
				filtered = append(filtered, n)
			}
		}
		t.receivedNums = filtered
	}

	// Never evict an acknowledged state without the sender's permission.
	if len(t.receivedNums) >= 1024 {
		return nil
	}
	// Track oldNum/newNum for state management.
	t.lastRecvOldNum = ti.OldNum
	t.lastRecvNewNum = ti.NewNum

	// Add new state.
	t.receivedNums = append(t.receivedNums, ti.NewNum)

	// Update ack num to latest received state.
	if ti.NewNum > t.ackNum {
		t.ackNum = ti.NewNum
	}

	// Trigger immediate ack when we receive data.
	if len(ti.Diff) > 0 {
		t.pendingDataAck = true
	}

	return &ti
}
