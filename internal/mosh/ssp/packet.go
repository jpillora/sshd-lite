// Adapted from mosh-go v0.5.2 (MIT); see ../LICENSE.mosh-go.
package ssp

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"time"
)

func (t *Transport) encryptFragment(f *Fragment, now time.Time) []byte {
	t.seqOut++
	seq := t.seqOut

	dirSeq := t.toRemote | (seq & SequenceMask)
	var dirSeqBytes [8]byte
	binary.BigEndian.PutUint64(dirSeqBytes[:], dirSeq)

	var nonce [12]byte
	copy(nonce[4:], dirSeqBytes[:])

	// Plaintext: [timestamp:2][timestamp_reply:2][fragment]
	fragWire := f.Marshal()
	ts := uint16(now.UnixMilli() & 0xffff)
	plaintext := make([]byte, 4+len(fragWire))
	binary.BigEndian.PutUint16(plaintext[0:], ts)
	reply := uint16(0xffff)
	if age := now.Sub(t.lastTSAt); !t.lastTSAt.IsZero() && age < time.Second && t.lastTS != 0xffff {
		reply = t.lastTS + uint16(age.Milliseconds())
		t.lastTSAt = time.Time{}
	}
	binary.BigEndian.PutUint16(plaintext[2:], reply)
	copy(plaintext[4:], fragWire)

	tagAndCT := t.ocb.Encrypt(nonce[:], plaintext)

	wire := make([]byte, 8+len(tagAndCT))
	copy(wire[:8], dirSeqBytes[:])
	copy(wire[8:], tagAndCT)
	return wire
}

// updateRTT updates the RTT estimate from a timestamp echo.
func (t *Transport) updateRTT(tsReply uint16) {
	now16 := uint16(time.Now().UnixMilli() & 0xffff)
	// Compute RTT in milliseconds, handling 16-bit wraparound.
	rttMS := int(now16) - int(tsReply)
	if rttMS < 0 {
		rttMS += 65536
	}
	if rttMS >= 5000 {
		return // implausible
	}
	rtt := time.Duration(rttMS) * time.Millisecond

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.rttInit {
		t.srtt = rtt
		t.rttvar = rtt / 2
		t.rttInit = true
	} else {
		// RFC 6298 Jacobson/Karels.
		delta := t.srtt - rtt
		if delta < 0 {
			delta = -delta
		}
		t.rttvar = (3*t.rttvar + delta) / 4
		t.srtt = (7*t.srtt + rtt) / 8
	}

	t.rto = t.srtt + 4*t.rttvar
	if t.rto < minRTO {
		t.rto = minRTO
	}
	if t.rto > maxRTO {
		t.rto = maxRTO
	}
}

// zlibCompress compresses data with zlib, reusing the writer.
func (t *Transport) zlibCompress(data []byte) []byte {
	t.zlibBuf.Reset()
	if t.zlibW == nil {
		t.zlibW = zlib.NewWriter(&t.zlibBuf)
	} else {
		t.zlibW.Reset(&t.zlibBuf)
	}
	t.zlibW.Write(data)
	t.zlibW.Close()
	out := make([]byte, t.zlibBuf.Len())
	copy(out, t.zlibBuf.Bytes())
	return out
}

// zlibDecompress decompresses zlib data. Returns nil on error.
// Limits output to 1 MiB to prevent decompression bombs.
func zlibDecompress(data []byte) []byte {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(out) > 1<<20 {
		return nil
	}
	return out
}
