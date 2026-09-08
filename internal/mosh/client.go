package mosh

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	wire "github.com/unixshells/mosh-go"
)

// RunClient runs an interactive session. Input and resize channels let callers
// own and cancel terminal reads without leaving goroutines behind here.
func RunClient(ctx context.Context, conn net.Conn, key string, input <-chan []byte, sizes <-chan Request, output io.Writer) (int, error) {
	ocb, err := wire.NewOCBFromBase64(key)
	if err != nil {
		return 0, err
	}
	tr := wire.NewTransport(ocb, false)
	ctx, cancel := context.WithCancel(ctx)
	received := make(chan []byte, 64)
	readerDone := make(chan struct{})
	defer func() { cancel(); conn.Close(); <-readerDone }()
	go func() {
		defer close(readerDone)
		b := make([]byte, 65536)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			n, err := conn.Read(b)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				// UDP errors (including ICMP unreachable) can be transient during roaming.
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			select {
			case received <- append([]byte(nil), b[:n]...):
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	tr.ForceNextSend()
	started := time.Now()
	connected := false
	var pending []wire.UserInstruction
	pendingBytes := 0
	var latestSize *Request
	flush := func() {
		for _, dg := range tr.Tick() {
			_, _ = conn.Write(dg)
		}
	}
	for {
		// Bound unsent input during outages; apply backpressure to the caller.
		readInput := input
		if pendingBytes >= 64*1024 {
			readInput = nil
		}
		select {
		case <-ctx.Done():
			// Best effort explicit close; loss still falls back to the idle timeout.
			tr.SetPending(wire.MarshalUserMessage([]wire.UserInstruction{{Control: &wire.LatchControl{Type: controlClose}}}))
			tr.ForceNextSend()
			flush()
			return 0, ctx.Err()
		case keys, ok := <-readInput:
			if !ok {
				input = nil
				continue
			}
			if len(keys) > 0 {
				pending = append(pending, wire.UserInstruction{Keys: keys})
				pendingBytes += len(keys)
			}
		case size, ok := <-sizes:
			if !ok {
				sizes = nil
				continue
			}
			if err := size.Validate(); err != nil {
				return 0, err
			}
			latestSize = &size
		case dg := <-received:
			before := tr.LastRecv()
			diff := tr.Recv(dg)
			if tr.LastRecv().After(before) {
				connected = true
			}
			if len(diff) == 0 {
				continue
			}
			instructions, err := wire.UnmarshalHostMessage(diff)
			if err != nil {
				return 0, fmt.Errorf("mosh server message: %w", err)
			}
			for _, instruction := range instructions {
				if len(instruction.Hoststring) > 0 {
					if _, err := output.Write(instruction.Hoststring); err != nil {
						return 0, err
					}
				}
				if instruction.Control != nil && instruction.Control.Type == controlExit {
					if len(instruction.Control.Payload) != 4 {
						return 0, errors.New("invalid mosh exit status")
					}
					tr.ForceNextSend()
					flush()
					return int(binary.BigEndian.Uint32(instruction.Control.Payload)), nil
				}
			}
		case <-ticker.C:
			if !connected && time.Since(started) >= 10*time.Second {
				return 0, errors.New("mosh UDP connection timed out (check the server's UDP port)")
			}
			if time.Since(tr.LastRecv()) >= IdleTimeout {
				return 0, errors.New("mosh session expired after five minutes without authenticated UDP traffic")
			}
			// At most one input state is in flight. Retain it in Transport until acked,
			// so retransmission never applies a keystroke twice.
			if tr.AckedByRemote() >= tr.SentNum() && (len(pending) > 0 || latestSize != nil) {
				if latestSize != nil {
					pending = append(pending, wire.UserInstruction{Width: int32(latestSize.Cols), Height: int32(latestSize.Rows)})
					latestSize = nil
				}
				tr.SetPending(wire.MarshalUserMessage(pending))
				pending = nil
				pendingBytes = 0
			}
			flush()
		}
	}
}
