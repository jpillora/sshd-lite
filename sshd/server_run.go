package sshd

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// serverRun owns only resources accepted by one StartWithContext invocation.
// Keeping this state off Server makes concurrent invocations and HandleConn
// independent: stopping one listener cannot close another run's transports.
type serverRun struct {
	listener net.Listener

	mu        sync.Mutex
	closing   bool
	conns     map[*trackedServerConn]struct{}
	connWG    sync.WaitGroup
	closeOnce sync.Once

	watchStop chan struct{}
	watchDone chan struct{}
}

type trackedServerConn struct {
	net.Conn
	closeOnce sync.Once
	closeErr  error
}

func (c *trackedServerConn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}

func newServerRun(listener net.Listener) *serverRun {
	return &serverRun{
		listener:  listener,
		conns:     make(map[*trackedServerConn]struct{}),
		watchStop: make(chan struct{}),
		watchDone: make(chan struct{}),
	}
}

func (r *serverRun) watch(ctx context.Context, onCancel func()) {
	go func() {
		defer close(r.watchDone)
		select {
		case <-ctx.Done():
			onCancel()
			r.shutdown()
		case <-r.watchStop:
		}
	}()
}

func (r *serverRun) stopWatching() {
	close(r.watchStop)
	<-r.watchDone
}

func (r *serverRun) track(conn net.Conn) (*trackedServerConn, bool) {
	tracked := &trackedServerConn{Conn: conn}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return nil, false
	}
	r.conns[tracked] = struct{}{}
	r.connWG.Add(1)
	return tracked, true
}

func (r *serverRun) untrack(conn *trackedServerConn) {
	r.mu.Lock()
	delete(r.conns, conn)
	r.mu.Unlock()
	r.connWG.Done()
}

func (r *serverRun) shutdown() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		connections := make([]*trackedServerConn, 0, len(r.conns))
		for tracked := range r.conns {
			connections = append(connections, tracked)
		}
		r.mu.Unlock()

		// Close may wake code that needs the lifecycle mutex, so never invoke
		// user-supplied Listener or Conn implementations while holding it.
		_ = r.listener.Close()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
}

func (r *serverRun) wait() {
	r.connWG.Wait()
}

func (s *Server) acquireHandshake() bool {
	if s.handshakes == nil {
		return true
	}
	select {
	case s.handshakes <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseHandshake() {
	if s.handshakes != nil {
		<-s.handshakes
	}
}

func (s *Server) debugf(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		// debug logs only emit if enabled on the slogger (verbose is enabled)
		s.config.Logger.Debug(fmt.Sprintf(f, args...))
	}
}

func (s *Server) infof(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		s.config.Logger.Info(fmt.Sprintf(f, args...))
	}
}

func (s *Server) errorf(f string, args ...interface{}) {
	if !s.config.LogQuiet {
		s.config.Logger.Error(fmt.Sprintf(f, args...))
	}
}
