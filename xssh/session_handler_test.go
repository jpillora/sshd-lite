package xssh

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestHandlePtyReqValidation(t *testing.T) {
	valid := marshalPtyRequest(80, 24, []byte{ssh.ECHO, 0, 0, 0, 1, 0})
	oversized := marshalPtyRequest(maxTerminalDimension+1, 24, []byte{0})
	zero := marshalPtyRequest(0, 24, []byte{0})

	// A maximum uint32 string length exercises the old 4+length overflow path.
	maliciousTermLength := []byte{0xff, 0xff, 0xff, 0xff}
	maliciousModesLength := append([]byte(nil), valid[:4+len("xterm")+16]...)
	maliciousModesLength = append(maliciousModesLength, 0xff, 0xff, 0xff, 0xff)

	tests := []struct {
		name       string
		payload    []byte
		wantErr    string
		wantResize bool
	}{
		{name: "valid", payload: valid, wantResize: true},
		{name: "too short", payload: []byte{0, 0, 0}, wantErr: "malformed pty-req"},
		{name: "overflow style term length", payload: maliciousTermLength, wantErr: "malformed pty-req"},
		{name: "overflow style modes length", payload: maliciousModesLength, wantErr: "malformed pty-req"},
		{name: "incomplete terminal mode", payload: marshalPtyRequest(80, 24, []byte{ssh.ECHO, 0, 0}), wantErr: "incomplete uint32"},
		{name: "missing terminal mode end", payload: marshalPtyRequest(80, 24, []byte{ssh.ECHO, 0, 0, 0, 1}), wantErr: "missing TTY_OP_END"},
		{name: "terminal mode trailing data", payload: marshalPtyRequest(80, 24, []byte{0, 1}), wantErr: "trailing bytes"},
		{name: "outer trailing data", payload: append(append([]byte(nil), valid...), 1), wantErr: "malformed pty-req"},
		{name: "oversized dimensions", payload: oversized, wantErr: "out of range"},
		{name: "zero dimension is ignored", payload: zero},
		{name: "undefined terminal mode stops parsing", payload: marshalPtyRequest(80, 24, []byte{160, 1, 2, 3, 4, 5}), wantResize: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Resizes: make(chan []byte, 1)}
			err := handlePtyReq(sess, requestWithPayload(tt.payload))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("handlePtyReq() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handlePtyReq() error = %v, want substring %q", err, tt.wantErr)
			}

			select {
			case payload := <-sess.Resizes:
				if !tt.wantResize {
					t.Fatalf("unexpected resize payload %x", payload)
				}
				ws, parseErr := parseDims(payload)
				if parseErr != nil {
					t.Fatalf("queued dimensions are invalid: %v", parseErr)
				}
				if ws.Cols != 80 || ws.Rows != 24 {
					t.Fatalf("queued size = %dx%d, want 80x24", ws.Cols, ws.Rows)
				}
			default:
				if tt.wantResize {
					t.Fatal("valid request did not queue a resize")
				}
			}
		})
	}
}

func TestHandleWindowChangeValidation(t *testing.T) {
	tests := []struct {
		name       string
		payload    []byte
		wantErr    string
		wantResize bool
	}{
		{name: "valid", payload: marshalWindowChange(132, 43), wantResize: true},
		{name: "too short", payload: marshalWindowChange(132, 43)[:15], wantErr: "malformed window-change"},
		{name: "trailing data", payload: append(marshalWindowChange(132, 43), 0), wantErr: "malformed window-change"},
		{name: "maximum cross-platform dimensions", payload: marshalWindowChange(maxTerminalDimension, maxTerminalDimension), wantResize: true},
		{name: "oversized columns", payload: marshalWindowChange(maxTerminalDimension+1, 43), wantErr: "out of range"},
		{name: "oversized rows", payload: marshalWindowChange(132, maxTerminalDimension+1), wantErr: "out of range"},
		{name: "oversized value is not hidden by zero", payload: marshalWindowChange(maxTerminalDimension+1, 0), wantErr: "out of range"},
		{name: "zero dimensions are ignored", payload: marshalWindowChange(0, 43)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Resizes: make(chan []byte, 1)}
			err := handleWindowChange(sess, requestWithPayload(tt.payload))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("handleWindowChange() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("handleWindowChange() error = %v, want substring %q", err, tt.wantErr)
			}

			select {
			case payload := <-sess.Resizes:
				if !tt.wantResize {
					t.Fatalf("unexpected resize payload %x", payload)
				}
				ws, parseErr := parseDims(payload)
				if parseErr != nil {
					t.Fatalf("queued dimensions are invalid: %v", parseErr)
				}
				wantCols, wantRows := uint16(132), uint16(43)
				if tt.name == "maximum cross-platform dimensions" {
					wantCols, wantRows = uint16(maxTerminalDimension), uint16(maxTerminalDimension)
				}
				if ws.Cols != wantCols || ws.Rows != wantRows {
					t.Fatalf("queued size = %dx%d, want %dx%d", ws.Cols, ws.Rows, wantCols, wantRows)
				}
			default:
				if tt.wantResize {
					t.Fatal("valid request did not queue a resize")
				}
			}
		})
	}
}

func TestWindowChangeBurstCoalescesWithoutBlocking(t *testing.T) {
	sess := &Session{Resizes: make(chan []byte, 1)}
	done := make(chan error, 1)
	go func() {
		for cols := uint32(1); cols <= 1000; cols++ {
			if err := handleWindowChange(sess, requestWithPayload(marshalWindowChange(cols, 24))); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resize burst failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resize burst blocked the request dispatcher")
	}

	payload := <-sess.Resizes
	ws, err := parseDims(payload)
	if err != nil {
		t.Fatalf("parse coalesced resize: %v", err)
	}
	if ws.Cols != 1000 || ws.Rows != 24 {
		t.Fatalf("coalesced size = %dx%d, want 1000x24", ws.Cols, ws.Rows)
	}
	select {
	case payload := <-sess.Resizes:
		t.Fatalf("queue retained a stale resize: %x", payload)
	default:
	}
}

func TestParseDimsRejectsUnvalidatedPublicPayloads(t *testing.T) {
	for _, payload := range [][]byte{
		nil,
		make([]byte, 7),
		make([]byte, 9),
		marshalWindowChange(80, 24),
	} {
		if _, err := parseDims(payload); err == nil {
			t.Fatalf("parseDims(%x) unexpectedly succeeded", payload)
		}
	}
}

func TestSetWinsizeRejectsNarrowingOverflowAndIgnoresZero(t *testing.T) {
	fake := fakeFdHolder(0)
	if err := SetWinsize(fake, maxTerminalDimension+1, 24); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("SetWinsize overflow error = %v", err)
	}
	if err := SetWinsize(fake, 80, maxTerminalDimension+1); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("SetWinsize overflow error = %v", err)
	}
	if err := SetWinsize(fake, 0, 24); err != nil {
		t.Fatalf("SetWinsize zero dimension should be ignored, got %v", err)
	}
}

func TestWinsizeCrossPlatformBoundary(t *testing.T) {
	ws, err := winsizeFromDimensions(maxTerminalDimension, maxTerminalDimension)
	if err != nil {
		t.Fatalf("maximum dimensions rejected: %v", err)
	}
	if ws.Cols != uint16(maxTerminalDimension) || ws.Rows != uint16(maxTerminalDimension) {
		t.Fatalf("maximum size = %dx%d", ws.Cols, ws.Rows)
	}
	if _, err := winsizeFromDimensions(maxTerminalDimension+1, 24); err == nil {
		t.Fatal("dimension 32768 was accepted despite Windows signed int16 backend")
	}
}

type fakeFdHolder uintptr

func (f fakeFdHolder) Fd() uintptr { return uintptr(f) }

func requestWithPayload(payload []byte) *Request {
	return WrapRequest(&ssh.Request{Payload: payload})
}

func marshalPtyRequest(cols, rows uint32, modes []byte) []byte {
	return ssh.Marshal(&ptyRequestPayload{
		Term:        "xterm",
		Columns:     cols,
		Rows:        rows,
		PixelWidth:  cols * 8,
		PixelHeight: rows * 16,
		Modes:       string(modes),
	})
}

func marshalWindowChange(cols, rows uint32) []byte {
	return ssh.Marshal(&windowChangePayload{
		Columns:     cols,
		Rows:        rows,
		PixelWidth:  cols * 8,
		PixelHeight: rows * 16,
	})
}

// Ensure the test helpers preserve the RFC's four uint32 window fields.
func TestMarshalWindowChangeHelper(t *testing.T) {
	payload := marshalWindowChange(80, 24)
	if len(payload) != 16 || binary.BigEndian.Uint32(payload[:4]) != 80 {
		t.Fatalf("invalid window-change helper payload: %x", payload)
	}
}
