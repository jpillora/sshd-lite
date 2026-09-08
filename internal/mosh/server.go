package mosh

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	wire "github.com/unixshells/mosh-go"
	vt "github.com/unixshells/vt-go"
)

type packet struct {
	data []byte
	addr *net.UDPAddr
}

type Server struct {
	conn     *net.UDPConn
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[*serverSession]struct{}
	wg       sync.WaitGroup
	idle     time.Duration
}

type serverSession struct {
	ocb       *wire.OCB
	transport *wire.Transport
	packets   chan packet
	cancel    context.CancelFunc
}

// Listen binds the same address and numeric port as the SSH listener.
func Listen(ctx context.Context, addr net.Addr) (*Server, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr.String())
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listen for mosh: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{conn: conn, ctx: ctx, cancel: cancel, sessions: make(map[*serverSession]struct{}), idle: IdleTimeout}
	s.wg.Add(1)
	go s.readLoop()
	return s, nil
}

func (s *Server) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.conn.Close()
	s.wg.Wait()
}

// Issue allocates a key after SSH authentication. The shell starts only after
// the first authenticated UDP packet; the session survives SSH disconnects.
func (s *Server) Issue(req Request, start StartTerminal) (Credentials, func(), error) {
	if err := req.Validate(); err != nil {
		return Credentials{}, nil, err
	}
	key, encoded, err := wire.GenerateKey()
	if err != nil {
		return Credentials{}, nil, err
	}
	ocb, err := wire.NewOCB(key)
	if err != nil {
		return Credentials{}, nil, err
	}
	ctx, cancel := context.WithCancel(s.ctx)
	session := &serverSession{ocb: ocb, transport: wire.NewTransport(ocb, true), packets: make(chan packet, 64), cancel: cancel}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || len(s.sessions) >= maxSessions {
		cancel()
		return Credentials{}, nil, errors.New("mosh unavailable or session limit reached")
	}
	s.sessions[session] = struct{}{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.sessions, session); s.mu.Unlock() }()
		s.serve(ctx, session, req, start)
	}()
	return Credentials{Port: s.conn.LocalAddr().(*net.UDPAddr).Port, Key: encoded}, cancel, nil
}

func (s *Server) readLoop() {
	defer s.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 28 || buf[0]&0x80 != 0 {
			continue
		}
		var nonce [12]byte
		copy(nonce[4:], buf[:8])
		s.mu.Lock()
		for session := range s.sessions {
			// Mosh has no session ID on the wire. Authenticate against the bounded
			// key set before routing; never trust a source address (clients roam).
			if session.ocb.Decrypt(nonce[:], buf[8:n]) == nil {
				continue
			}
			p := packet{data: append([]byte(nil), buf[:n]...), addr: addr}
			select {
			case session.packets <- p:
			default:
			}
			break
		}
		s.mu.Unlock()
	}
}

func (s *Server) serve(ctx context.Context, session *serverSession, req Request, start StartTerminal) {
	tr := session.transport
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var terminal Terminal
	var remote *net.UDPAddr
	var output <-chan []byte
	var exited <-chan int
	input := make(chan []byte, 64)
	var workers sync.WaitGroup
	workCtx, stop := context.WithCancel(ctx)
	defer func() {
		stop()
		if terminal != nil {
			terminal.Close()
		}
		workers.Wait()
	}()
	var emu *vt.Emulator
	visible := true
	var base, sent *wire.Framebuffer
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
			before := tr.LastRecv()
			diff := tr.Recv(p.data)
			if !tr.LastRecv().After(before) {
				continue
			}
			if remote == nil || remote.String() != p.addr.String() {
				tr.ForceNextSend()
			}
			remote = p.addr
			if terminal == nil && !ended {
				var err error
				terminal, err = start()
				if err != nil {
					ended = true
					exitCode = 1
					exitAt = time.Now()
				} else {
					emu = vt.NewEmulator(int(req.Cols), int(req.Rows))
					emu.SetCallbacks(vt.Callbacks{CursorVisibility: func(v bool) { visible = v }})
					base = wire.NewFramebuffer(int(req.Cols), int(req.Rows))
					out := make(chan []byte, 64)
					done := make(chan int, 1)
					output, exited = out, done
					workers.Add(2)
					go func() {
						defer workers.Done()
						defer close(out)
						b := make([]byte, 8192)
						for {
							n, err := terminal.Read(b)
							if n > 0 {
								select {
								case out <- append([]byte(nil), b[:n]...):
								case <-workCtx.Done():
									return
								}
							}
							if err != nil {
								return
							}
						}
					}()
					go func() {
						defer workers.Done()
						for {
							select {
							case <-workCtx.Done():
								return
							case b := <-input:
								if _, err := terminal.Write(b); err != nil {
									return
								}
							}
						}
					}()
					// Wait owns process reaping; Terminal.Close also waits for it.
					go func() { done <- terminal.Wait() }()
				}
			}
			if len(diff) == 0 || ended {
				continue
			}
			// User keystrokes and HostBytes share fields 2/4; resize and control
			// messages also share their schema. Upstream only exports this decoder.
			instructions, err := wire.UnmarshalHostMessage(diff)
			if err != nil {
				continue
			}
			for _, instruction := range instructions {
				if instruction.Control != nil && instruction.Control.Type == controlClose {
					return
				}
				if len(instruction.Hoststring) > 0 {
					select {
					case input <- instruction.Hoststring:
					case <-ctx.Done():
						return
					default:
						return
					}
				}
				if instruction.Width != 0 || instruction.Height != 0 {
					if instruction.Width < 1 || instruction.Height < 1 || instruction.Width > 1000 || instruction.Height > 1000 {
						continue
					}
					resize := Request{Cols: uint16(instruction.Width), Rows: uint16(instruction.Height)}
					if resize.Validate() != nil {
						continue
					}
					if terminal.Resize(resize.Cols, resize.Rows) == nil {
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
			acked := tr.AckedByRemote() >= tr.SentNum()
			if acked && sent != nil {
				base = sent
				sent = nil
			}
			if exitSent && acked {
				return
			}
			// Allow the PTY reader to drain before sending the final screen and exit.
			if acked && (dirty || (ended && output == nil && !exitSent)) {
				var instructions []wire.HostInstruction
				if dirty {
					sent = wire.SnapshotEmulator(emu, visible)
					instructions = append(instructions, wire.HostInstruction{Hoststring: sent.Diff(base), EchoAckNum: -1})
					dirty = false
				}
				if ended && output == nil {
					b := make([]byte, 4)
					binary.BigEndian.PutUint32(b, uint32(exitCode))
					instructions = append(instructions, wire.HostInstruction{EchoAckNum: -1, Control: &wire.LatchControl{Type: controlExit, Payload: b}})
					exitSent = true
				}
				tr.SetPending(wire.MarshalHostMessage(instructions))
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
