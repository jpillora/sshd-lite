package smux

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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
	}
}

func (s *session) executeInitialCommand(initialCommand string) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.PTY.Write([]byte(initialCommand + "\n"))
	}()
}

func (s *session) startShell() error {
	s.Command = exec.Command("/bin/bash")
	s.Command.Env = os.Environ()

	var err error
	s.PTY, err = pty.Start(s.Command)
	if err != nil {
		return err
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
	log.Printf("Client %s attached to session %s", clientID, s.ID)

	go s.handleClient(client)
}

func (s *session) RemoveClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.clients[clientID]; exists {
		delete(s.clients, clientID)
		log.Printf("Client %s detached from session %s", clientID, s.ID)
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
	return pty.Setsize(s.PTY, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
}

func (s *session) GetPTY() pty.Pty {
	return s.PTY
}

func (s *session) GetPTYSession() *sshclient.PTYSession {
	return sshclient.NewPTYSession(s.PTY)
}