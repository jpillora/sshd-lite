package mosh

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/display"
	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

func (session *serverSession) run(ctx context.Context, s *Server, req Request, start StartTerminal) {
	tr := session.transport
	tr.SetCaps([]byte{0x80})
	inputs := newInputStates()
	var cursorInput display.CursorKeys
	var echoNum uint64
	type echoPending struct {
		num uint64
		at  time.Time
	}
	var echoes []echoPending
	echoReady := false
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var terminal *terminalIO
	var remote *net.UDPAddr
	var remoteSeq uint64
	var shutdownAt time.Time
	var output <-chan []byte
	var exited <-chan int
	var emu *display.Screen
	defer func() {
		if terminal != nil {
			terminal.Close()
		}
	}()
	var base, sent *display.State
	dirty, ended, exitSent := false, false, false
	exitCode := 0
	var exitAt time.Time
	// LastRecv starts at issue time and only advances for a fresh authenticated
	// datagram. Outbound traffic, replays, and failed decrypts cannot renew a key.
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-session.packets:
			if time.Since(tr.LastRecv()) >= s.idle {
				return
			}
			update, fresh := tr.RecvUpdate(p.data)
			if !fresh {
				continue
			}
			seq := binary.BigEndian.Uint64(p.data[:8]) & ssp.SequenceMask
			if remote == nil || seq > remoteSeq {
				if remote == nil || remote.String() != p.addr.String() {
					tr.ForceNextSend()
				}
				remote, remoteSeq = p.addr, seq
			}
			if tr.RemoteShutdown() {
				if shutdownAt.IsZero() {
					shutdownAt = time.Now()
				}
				if terminal != nil {
					terminal.stopProcess()
				}
				tr.ForceNextSend()
				for _, dg := range tr.Tick() {
					s.conn.WriteToUDP(dg, remote)
				}
				continue
			}
			if terminal == nil && !ended {
				var err error
				terminal, err = startTerminalIO(ctx, req, start, session.cancel)
				if err != nil {
					ended = true
					exitCode = 1
					exitAt = time.Now()
				} else {
					emu = terminal.screen
					base = display.Blank(int(req.Cols), int(req.Rows))
					output, exited = terminal.output, terminal.exited
				}
			}
			if update == nil || ended {
				continue
			}
			instructions, err := inputs.apply(update)
			if err != nil {
				return
			}
			if len(echoes) >= 1024 {
				return
			}
			if update.NewNum > echoNum {
				echoes = append(echoes, echoPending{update.NewNum, time.Now().Add(50 * time.Millisecond)})
			}
			for _, instruction := range instructions {
				keys := cursorInput.Translate(instruction.Keys, emu.ApplicationCursor())
				if len(keys) > 0 && !terminal.queueInput(keys) {
					return
				}
				if instruction.Width != 0 || instruction.Height != 0 {
					if instruction.Width < 1 || instruction.Height < 1 || instruction.Width > 1000 || instruction.Height > 1000 {
						continue
					}
					resize := Request{Cols: uint16(instruction.Width), Rows: uint16(instruction.Height)}
					if resize.Validate() != nil {
						continue
					}
					if terminal.terminal.Resize(resize.Cols, resize.Rows) == nil {
						emu.Resize(int(resize.Cols), int(resize.Rows))
						dirty = true
					}
				}
			}
		case data, ok := <-output:
			if !ok {
				output = nil
			} else {
				emu.Write(data)
				dirty = true
			}
		case exitCode = <-exited:
			exited = nil
			ended = true
			exitAt = time.Now()
		case <-ticker.C:
			if time.Since(tr.LastRecv()) >= s.idle {
				return
			}
			if remote == nil {
				continue
			}
			if !shutdownAt.IsZero() {
				if time.Since(shutdownAt) >= 3*time.Second {
					return
				}
				continue
			}
			acked := tr.AckedByRemote() >= tr.SentNum()
			if acked && sent != nil {
				base = sent
				sent = nil
			}
			if tr.ShutdownAcked() {
				return
			}
			if exitSent && acked {
				tr.StartShutdown()
			}
			for len(echoes) > 0 && !time.Now().Before(echoes[0].at) {
				echoNum = max(echoNum, echoes[0].num)
				echoes = echoes[1:]
				echoReady = true
			}
			if echoReady && acked {
				dirty = true
			}
			// Allow the PTY reader to drain before sending the final screen and exit.
			if !exitSent && acked && (dirty || (ended && output == nil)) {
				var instructions []wire.HostInstruction
				if dirty {
					sent = emu.Snapshot()
					instructions = append(instructions, wire.HostInstruction{Hoststring: sent.Diff(base), EchoAckNum: -1})
					if sent.Columns() != base.Columns() || sent.Rows() != base.Rows() {
						instructions = append([]wire.HostInstruction{{Width: int32(sent.Columns()), Height: int32(sent.Rows()), EchoAckNum: -1}}, instructions...)
					}
					if echoReady {
						instructions = append(instructions, wire.HostInstruction{EchoAckNum: int64(echoNum)})
						echoReady = false
					}
					dirty = false
				}
				if ended && output == nil {
					b := make([]byte, 4)
					binary.BigEndian.PutUint32(b, uint32(exitCode))
					if tr.HasCap(0x80) {
						instructions = append(instructions, wire.HostInstruction{EchoAckNum: -1, Control: &wire.LatchControl{Type: controlExit, Payload: b}})
					}
					exitSent = true
				}
				tr.SetPending(wire.MarshalHostMessage(instructions))
				if exitSent && len(instructions) == 0 {
					tr.StartShutdown()
				}
			}
			// Tick includes encrypted keepalives, acknowledgements and retransmits.
			for _, dg := range tr.Tick() {
				s.conn.WriteToUDP(dg, remote)
			}
			if ended && time.Since(exitAt) > 5*time.Second {
				return
			}
		}
	}
}
