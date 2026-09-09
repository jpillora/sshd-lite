package mosh

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/display"
	"github.com/jpillora/sshd-lite/internal/mosh/ssp"

	wire "github.com/unixshells/mosh-go"
)

// RunClient runs an interactive session. Input and resize channels let callers
// own and cancel terminal reads without leaving goroutines behind here.
func RunClient(ctx context.Context, conn net.Conn, key string, initial Request, input <-chan []byte, sizes <-chan Request, output io.Writer) (int, error) {
	if err := initial.Validate(); err != nil {
		return 0, err
	}
	rawKey, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(key, "="))
	if err != nil {
		return 0, errors.New("invalid Mosh key encoding")
	}
	ocb, err := wire.NewOCB(rawKey)
	if err != nil {
		return 0, err
	}
	tr := ssp.NewTransport(ocb, false)
	networkCtx, cancel := context.WithCancel(context.Background())
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
				if networkCtx.Err() != nil {
					return
				}
				// UDP errors (including ICMP unreachable) can be transient during roaming.
				select {
				case <-networkCtx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			select {
			case received <- append([]byte(nil), b[:n]...):
			case <-networkCtx.Done():
				return
			}
		}
	}()
	tr.SetCaps([]byte{0x80})
	states := display.NewReceiver(int(initial.Cols), int(initial.Rows))
	defer states.Close()
	done := ctx.Done()
	var closingAt time.Time
	var closeErr error
	exitCode := 0
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
		case <-done:
			done = nil
			input = nil
			sizes = nil
			pending = nil
			closingAt = time.Now()
			closeErr = ctx.Err()
			tr.StartShutdown()
			flush()
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
			update, fresh := tr.RecvUpdate(dg)
			if fresh {
				connected = true
			}
			if update == nil {
				continue
			}
			out, err := states.Apply(update)
			if err != nil {
				return 0, err
			}
			if len(out) > 0 {
				if n, err := output.Write(out); err != nil {
					return 0, err
				} else if n != len(out) {
					return 0, io.ErrShortWrite
				}
			}
			messages, err := wire.UnmarshalHostMessage(update.Diff)
			if err != nil {
				return 0, err
			}
			for _, m := range messages {
				if m.Control != nil && m.Control.Type == controlExit && tr.HasCap(0x80) {
					if len(m.Control.Payload) != 4 {
						return 0, errors.New("invalid mosh exit status")
					}
					exitCode = int(binary.BigEndian.Uint32(m.Control.Payload))
				}
			}
			if tr.RemoteShutdown() {
				tr.ForceNextSend()
				flush()
				return exitCode, nil
			}
		case <-ticker.C:
			if tr.ShutdownAcked() || (!closingAt.IsZero() && time.Since(closingAt) > 3*time.Second) {
				return 0, closeErr
			}
			if !connected && time.Since(started) >= 10*time.Second {
				return 0, errors.New("mosh UDP connection timed out (check the server's UDP port)")
			}
			if time.Since(tr.LastRecv()) >= IdleTimeout {
				return 0, errors.New("mosh session expired after five minutes without authenticated UDP traffic")
			}
			// At most one input state is in flight. Retain it in ssp.Transport until acked,
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
