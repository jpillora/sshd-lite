package xssh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// cmd.exe takes /c where every other supported shell takes -c. Getting this
// wrong does not fail loudly: cmd.exe treats -c as nothing it recognises and
// waits on input that never arrives, so exec sessions hang until the client
// times out rather than reporting an error.
func TestCommandFlagPerShell(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"bash", "-c"},
		{"/bin/bash", "-c"},
		{"/usr/bin/fish", "-c"},
		{"sh", "-c"},
		{"powershell", "-c"},
		{`C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, "-c"},
		{"pwsh", "-c"},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, "-c"},
		{"cmd", "/c"},
		{"cmd.exe", "/c"},
		{`C:\WINDOWS\system32\cmd.exe`, "/c"},
		{`C:\WINDOWS\SYSTEM32\CMD.EXE`, "/c"},
	}
	for _, tt := range tests {
		if got := commandFlag(tt.shell); got != tt.want {
			t.Errorf("commandFlag(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestShellBaseNormalisesPathAndSuffix(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"bash", "bash"},
		{"/bin/bash", "bash"},
		{`C:\WINDOWS\system32\cmd.exe`, "cmd"},
		{"CMD.EXE", "cmd"},
		{`C:\Program Files\Git\bin\bash.exe`, "bash"},
	}
	for _, tt := range tests {
		if got := shellBase(tt.shell); got != tt.want {
			t.Errorf("shellBase(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestCommandWaitDelayBoundsInheritedOutput(t *testing.T) {
	const modeEnv = "SSHD_LITE_WAIT_DELAY_HELPER"
	const pidFileEnv = "SSHD_LITE_WAIT_DELAY_PID_FILE"
	switch os.Getenv(modeEnv) {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestCommandWaitDelayBoundsInheritedOutput$")
		child.Env = append(os.Environ(), modeEnv+"=child")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatalf("start output-holding child: %v", err)
		}
		if err := os.WriteFile(os.Getenv(pidFileEnv), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			_ = child.Process.Kill()
			t.Fatalf("write child pid: %v", err)
		}
		return
	case "child":
		time.Sleep(30 * time.Second)
		return
	}

	pidFile := fmt.Sprintf("%s/child.pid", t.TempDir())
	cmd := exec.Command(os.Args[0], "-test.run=^TestCommandWaitDelayBoundsInheritedOutput$")
	cmd.Env = append(os.Environ(), modeEnv+"=parent", pidFileEnv+"="+pidFile)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	prepareCommand(cmd)
	if cmd.WaitDelay <= 0 {
		t.Fatal("remote command has no bounded I/O wait")
	}
	// Keep the regression fast while exercising the same configured mechanism.
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper parent: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for childPID == 0 {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			childPID, err = strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("parse child pid: %v", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read child pid: %v", err)
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("timed out waiting for helper pid; output: %s", output.String())
		}
		if childPID == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if process, err := os.FindProcess(childPID); err == nil {
				_ = process.Kill()
			}
		})
	}
	t.Cleanup(cleanup)

	started := time.Now()
	err := cmd.Wait()
	if !errors.Is(err, exec.ErrWaitDelay) {
		cleanup()
		t.Fatalf("Wait error = %v, want exec.ErrWaitDelay; output: %s", err, output.String())
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		cleanup()
		t.Fatalf("WaitDelay did not bound inherited output pipe: %s", elapsed)
	}
	cleanup()
}

func TestCommandExitCode(t *testing.T) {
	// A command that exits non-zero yields an *exec.ExitError carrying its code.
	failing := exec.Command(os.Args[0], "-test.run=^TestCommandExitCodeHelper$")
	failing.Env = append(os.Environ(), "SSHD_LITE_EXIT_CODE_HELPER=3")
	failingErr := failing.Run()

	tests := []struct {
		name string
		err  error
		want uint32
	}{
		{name: "success", err: nil, want: 0},
		{name: "exit error keeps its code", err: failingErr, want: 3},
		// Wait substitutes ErrWaitDelay only for a nil error, so the command
		// succeeded and merely left inherited output pipes open.
		{name: "wait delay is not a failure", err: exec.ErrWaitDelay, want: 0},
		{name: "wrapped wait delay is not a failure", err: fmt.Errorf("wait: %w", exec.ErrWaitDelay), want: 0},
		{name: "other errors report failure", err: errors.New("boom"), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandExitCode(tt.err); got != tt.want {
				t.Fatalf("commandExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestCommandExitCodeHelper(t *testing.T) {
	code, ok := os.LookupEnv("SSHD_LITE_EXIT_CODE_HELPER")
	if !ok {
		t.Skip("helper process only")
	}
	status, err := strconv.Atoi(code)
	if err != nil {
		t.Fatalf("invalid helper exit code %q: %v", code, err)
	}
	os.Exit(status)
}

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
		// libssh2 sends an empty modes string when its caller supplies no modes,
		// and OpenSSH's parser also stops at the end of the buffer. Requiring
		// TTY_OP_END here would reject pty-req from those clients.
		{name: "empty terminal modes", payload: marshalPtyRequest(80, 24, nil), wantResize: true},
		{name: "terminal modes without end", payload: marshalPtyRequest(80, 24, []byte{ssh.ECHO, 0, 0, 0, 1}), wantResize: true},
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
