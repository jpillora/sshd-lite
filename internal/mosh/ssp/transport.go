// Adapted from mosh-go v0.5.2 (MIT); see ../LICENSE.mosh-go.
package ssp

import (
	"bytes"
	"compress/zlib"
	"sync"
	"time"

	wire "github.com/unixshells/mosh-go"
)

type OCB = wire.OCB
type Fragment = wire.Fragment

const (
	dirToClient        = uint64(1) << 63
	dirToServer        = uint64(0)
	SequenceMask       = dirToClient - 1
	minDatagram        = 24
	fragmentHeaderSize = 10
	shutdownNum        = ^uint64(0)
)

type Update = wire.TransportInstruction

// Transport implements the mosh State Synchronization Protocol (SSP).
//
// It manages sequence numbering, acknowledgements, retransmission timing,
// and the fragment/encrypt/decrypt pipeline. Both client and server use
// the same Transport with different direction bits.
//
// The caller provides state diffs (server: terminal output, client: keystrokes)
// and receives remote state updates.
type Transport struct {
	mu sync.Mutex

	ocb      *OCB
	toRemote uint64 // direction bit for outgoing (dirToServer or dirToClient)
	toLocal  uint64 // direction bit for incoming

	// Outgoing state (SSP §3).
	sentNum        uint64 // newest state we've sent (new_num)
	ackedByRemote  uint64 // newest state the remote has acknowledged
	pendingDiff    []byte // diff payload waiting to be sent
	diffSent       bool   // true = pendingDiff has been sent at least once
	diffOldNum     uint64 // locked oldNum for all diffs until base advances
	hasPendingBase bool   // true = diffOldNum is locked
	pendingDataAck bool   // true = send ack ASAP (received data, not just ack)

	// Incoming state — list of received state nums for old_num validation.
	receivedNums   []uint64 // ordered list of state nums we have
	ackNum         uint64   // latest received state num
	sentAckNum     uint64   // last ackNum we actually sent on wire
	throwawayNum   uint64   // oldest state we still hold
	lastRecvOldNum uint64   // oldNum from most recently received diff
	lastRecvNewNum uint64   // newNum from most recently received diff

	// Sequence counter for the crypto layer (independent of SSP state numbering).
	seqOut      uint64
	seqInMax    uint64
	seqInMaxSet bool         // false until first datagram received
	seqSeen     [1024]uint64 // sequence + 1, a bounded authenticated replay window

	// Timestamps.
	lastSend time.Time
	lastRecv time.Time
	lastTS   uint16 // last remote timestamp for echo
	lastTSAt time.Time

	// RTT estimation (Jacobson/Karels).
	srtt    time.Duration
	rttvar  time.Duration
	rto     time.Duration
	rttInit bool

	// Fragment assembler for incoming.
	assembler       wire.FragmentAssembler
	instructionID   uint64
	lastInstruction []byte
	shutdown        bool

	// Latch capability negotiation.
	localCaps  []byte
	remoteCaps []byte

	// Reusable zlib writer to avoid per-tick allocations.
	zlibBuf bytes.Buffer
	zlibW   *zlib.Writer
}

const (
	initialRTO = 1000 * time.Millisecond
	minRTO     = 250 * time.Millisecond
	maxRTO     = 10 * time.Second
)

// NewTransport creates a transport. isServer determines direction bits.
func NewTransport(ocb *OCB, isServer bool) *Transport {
	t := &Transport{
		ocb:          ocb,
		rto:          initialRTO,
		lastSend:     time.Now(),
		lastRecv:     time.Now(),
		receivedNums: []uint64{0}, // start with state 0
	}
	if isServer {
		t.toRemote = dirToClient
		t.toLocal = dirToServer
	} else {
		t.toRemote = dirToServer
		t.toLocal = dirToClient
	}
	return t
}

func (t *Transport) SetCaps(caps []byte) {
	t.mu.Lock()
	t.localCaps = caps
	t.mu.Unlock()
}

func (t *Transport) RemoteCaps() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remoteCaps
}

func (t *Transport) HasCap(bit byte) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.localCaps) == 0 || len(t.remoteCaps) == 0 {
		return false
	}
	idx := 0
	if idx >= len(t.localCaps) || idx >= len(t.remoteCaps) {
		return false
	}
	return (t.localCaps[idx] & t.remoteCaps[idx] & bit) != 0
}

// ForceNextSend forces the next tick to send, even with no pending diff.
func (t *Transport) ForceNextSend() {
	t.mu.Lock()
	t.lastSend = time.Time{} // zero time, always expired
	t.mu.Unlock()
}

// SetPending sets the diff payload to send on the next tick.
func (t *Transport) SetPending(diff []byte) {
	t.mu.Lock()
	if t.shutdown {
		t.mu.Unlock()
		return
	}
	if len(diff) > 0 {
		t.diffSent = false
	}
	t.pendingDiff = diff
	t.mu.Unlock()
}

// Tick produces outgoing wire datagrams if it's time to send.
// Returns nil if nothing to send.
func (t *Transport) Tick() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	// Decide if we should send.
	haveDiff := len(t.pendingDiff) > 0
	haveNewDiff := haveDiff && !t.diffSent
	if t.shutdown && t.sentNum != shutdownNum {
		haveNewDiff = true
	}
	needAck := t.ackNum > t.sentAckNum
	sinceLastSend := now.Sub(t.lastSend)
	retry := t.rto
	if t.shutdown {
		retry = minRTO
	}
	expired := sinceLastSend >= retry
	urgentAck := t.pendingDataAck

	shouldSend := haveNewDiff || needAck || expired || urgentAck
	if !shouldSend {
		return nil
	}

	// Build TransportInstruction.
	if haveNewDiff {
		if t.shutdown {
			t.sentNum = shutdownNum
		} else {
			t.sentNum++
		}
		t.diffSent = true
		if !t.hasPendingBase {
			t.diffOldNum = t.ackedByRemote
			t.hasPendingBase = true
		}
	}
	t.pendingDataAck = false

	oldNum := t.ackedByRemote
	if haveDiff {
		oldNum = t.diffOldNum // use locked oldNum for diff retransmissions
	}

	ti := wire.TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          oldNum,
		NewNum:          t.sentNum,
		AckNum:          t.ackNum,
		ThrowawayNum:    oldNum, // never reference states older than our retained base
		Diff:            t.pendingDiff,
		LatchCaps:       t.localCaps,
	}
	t.sentAckNum = t.ackNum
	// Do NOT nil pendingDiff — keep for retransmission until server acks.

	// Marshal → compress → fragment → encrypt.
	pbData := ti.Marshal()
	compressed := t.zlibCompress(pbData)
	if !bytes.Equal(pbData, t.lastInstruction) {
		t.instructionID++
		t.lastInstruction = append(t.lastInstruction[:0], pbData...)
	}
	frags := wire.Fragmentize(t.instructionID, compressed)

	var datagrams [][]byte
	for i := range frags {
		wire := t.encryptFragment(&frags[i], now)
		datagrams = append(datagrams, wire)
	}

	t.lastSend = now
	return datagrams
}

// Recv processes an incoming wire datagram.
// Returns the diff payload if a complete message was reassembled, or nil.
// AckedByRemote returns the highest state number the remote has acked.
func (t *Transport) AckedByRemote() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ackedByRemote
}

// SentNum returns the current sent state number.
func (t *Transport) SentNum() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sentNum
}

// LastRecvOldNum returns the oldNum from the most recently received diff.
func (t *Transport) LastRecvOldNum() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastRecvOldNum
}

// LastRecvNewNum returns the newNum from the most recently received diff.
func (t *Transport) LastRecvNewNum() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastRecvNewNum
}

// ThrowawayNum returns the server's throwaway number — states below this
// are no longer referenced by the server and can be safely pruned.
func (t *Transport) ThrowawayNum() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.throwawayNum
}

// LastRecv returns the time of the last received datagram.
func (t *Transport) LastRecv() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastRecv
}

// RTO returns the current retransmission timeout.
func (t *Transport) RTO() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rto
}

// encryptFragment encrypts a fragment and wraps it in the mosh wire format.
// Caller holds t.mu.
func (t *Transport) StartShutdown() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shutdown = true
	t.lastSend = time.Time{}
}
func (t *Transport) RemoteShutdown() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ackNum == shutdownNum
}
func (t *Transport) ShutdownAcked() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.shutdown && t.ackedByRemote == shutdownNum
}

// replayed is called with mu held. Out-of-order fragments are valid, but a
// repeated nonce or a packet outside the window cannot renew the session.
func (t *Transport) replayed(seq uint64) bool {
	return t.seqInMaxSet && ((seq <= t.seqInMax && t.seqInMax-seq >= uint64(len(t.seqSeen))) || t.seqSeen[seq%uint64(len(t.seqSeen))] == seq+1)
}
