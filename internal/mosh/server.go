package mosh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"

	wire "github.com/unixshells/mosh-go"
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
	transport *ssp.Transport
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

func (s *Server) Port() int { return s.conn.LocalAddr().(*net.UDPAddr).Port }

func (s *Server) Close() error {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.conn.Close()
	s.wg.Wait()
	return nil
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
	session := &serverSession{ocb: ocb, transport: ssp.NewTransport(ocb, true), packets: make(chan packet, 64), cancel: cancel}
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
		session.run(ctx, s, req, start)
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
