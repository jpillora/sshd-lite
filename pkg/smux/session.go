package smux

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
	sshclient "github.com/jpillora/sshd-lite/pkg/client"
)

type session struct {
	ID        string
	StartTime time.Time
	Command   *exec.Cmd
	PTY       pty.Pty
	clients   map[string]*sessionClient
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *slog.Logger
}

type sessionClient struct {
	ID     string
	Writer io.Writer
	Reader io.Reader
}

func newSession(id string) *session {
	ctx, cancel := context.WithCancel(context.Background())
	return &session{
		ID:        id,
		StartTime: time.Now(),
		clients:   make(map[string]*sessionClient),
		ctx:       ctx,
		cancel:    cancel,
		logger:    slog.Default(), // Use default logger for sessions
	}
}

func (s *session) executeInitialCommand(initialCommand string) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.PTY.Write([]byte(initialCommand + "\n"))
	}()
}

func (s *session) startShell() error {
	s.Command = exec.Command(getDefaultShell())
	s.Command.Env = append(os.Environ(), "SMUX_SESSION_NAME="+s.ID)

	// Use a timeout to prevent hanging on Windows PTY issues
	done := make(chan error, 1)
	go func() {
		var err error
		s.PTY, err = pty.Start(s.Command)
		done <- err
	}()
	
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout starting shell (PTY may not be supported on this platform)")
	}

	go s.monitor()
	return nil
}

func (s *session) monitor() {
	defer func() {
		s.terminate()
	}()

	if s.Command.Process != nil {
		s.Command.Process.Wait()
	}
}

func (s *session) terminate() {
	s.cancel()
	
	s.mu.Lock()
	defer s.mu.Unlock()

	for clientID := range s.clients {
		delete(s.clients, clientID)
	}

	if s.PTY != nil {
		s.PTY.Close()
	}

	if s.Command != nil && s.Command.Process != nil {
		s.Command.Process.Kill()
	}
}

func (s *session) AddClient(clientID string, writer io.Writer, reader io.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client := &sessionClient{
		ID:     clientID,
		Writer: writer,
		Reader: reader,
	}

	s.clients[clientID] = client
	s.logger.Debug("Client attached to session", "client_id", clientID, "session_id", s.ID)

	go s.handleClient(client)
}

func (s *session) RemoveClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.clients[clientID]; exists {
		delete(s.clients, clientID)
		s.logger.Debug("Client detached from session", "client_id", clientID, "session_id", s.ID)
	}
}

func (s *session) handleClient(client *sessionClient) {
	defer s.RemoveClient(client.ID)

	go func() {
		io.Copy(client.Writer, s.PTY)
	}()

	io.Copy(s.PTY, client.Reader)
}

func (s *session) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *session) Resize(rows, cols int) error {
	if s.PTY == nil {
		return fmt.Errorf("no PTY available")
	}
	
	// Use timeout for PTY operations that may hang on Windows
	done := make(chan error, 1)
	go func() {
		err := pty.Setsize(s.PTY, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
		done <- err
	}()
	
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timeout resizing PTY (operation may not be supported on this platform)")
	}
}

func (s *session) GetPTY() pty.Pty {
	return s.PTY
}

func (s *session) GetPTYSession() *sshclient.PTYSession {
	return sshclient.NewPTYSession(s.PTY)
}

func getDefaultShell() string {
	shell := os.Getenv("SHELL")
	if shell != "" {
		if _, err := exec.LookPath(shell); err == nil {
			return shell
		}
	}
	
	if runtime.GOOS == "windows" {
		shell = "powershell"
	} else {
		shell = "bash"
	}
	
	if p, err := exec.LookPath(shell); err == nil {
		return p
	}
	
	return "/bin/bash"
}