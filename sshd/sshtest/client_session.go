package sshtest

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"golang.org/x/crypto/ssh"
)

// sessionGo implements Session.
type sessionGo struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	output  *outputCapture
	events  *EventBus
	name    string
	cols    uint32
	rows    uint32
}

// outputCapture captures output in a thread-safe way.
type outputCapture struct {
	mu   sync.RWMutex
	data []byte
}

func (o *outputCapture) Write(p []byte) (n int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = append(o.data, p...)
	return len(p), nil
}

func (o *outputCapture) String() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return string(o.data)
}

func (s *sessionGo) captureOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.stdout.Read(buf)
		if n > 0 {
			s.output.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *sessionGo) Read(p []byte) (n int, err error) {
	return s.stdout.Read(p)
}

func (s *sessionGo) Write(p []byte) (n int, err error) {
	return s.stdin.Write(p)
}

func (s *sessionGo) Close() error {
	s.events.Emit(scenario.EventShellEnded, "client", s.name)
	return s.session.Close()
}

func (s *sessionGo) Resize(cols, rows uint32) error {
	s.cols = cols
	s.rows = rows
	err := s.session.WindowChange(int(rows), int(cols))
	if err == nil {
		s.events.Emit(scenario.EventPTYResized, "client", s.name, "cols", fmt.Sprint(cols), "rows", fmt.Sprint(rows))
	}
	return err
}

func (s *sessionGo) Output() string {
	return s.output.String()
}

func (s *sessionGo) WaitForOutput(text string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Output(), text) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %q in output", text)
}
