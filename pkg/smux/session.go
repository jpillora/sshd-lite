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
	"github.com/jpillora/sshd-lite/pkg/client"
)

type Session struct {
	ID        string
	Name      string
	StartTime time.Time
	Command   *exec.Cmd
	PTY       pty.Pty
	clients   map[string]*Client
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
}

type Client struct {
	ID     string
	Writer io.Writer
	Reader io.Reader
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) CreateSession(id, name string) (*Session, error) {
	return sm.CreateSessionWithCommand(id, name, "")
}

func (sm *SessionManager) CreateSessionWithCommand(id, name, initialCommand string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[id]; exists {
		return nil, fmt.Errorf("session %s already exists", id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		ID:        id,
		Name:      name,
		StartTime: time.Now(),
		clients:   make(map[string]*Client),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start the shell process
	if err := session.startShell(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start shell: %v", err)
	}

	// If there's an initial command, send it to the shell
	if initialCommand != "" {
		go func() {
			// Give the shell a moment to start
			time.Sleep(100 * time.Millisecond)
			session.PTY.Write([]byte(initialCommand + "\n"))
		}()
	}

	sm.sessions[id] = session
	log.Printf("Created session %s (%s)", id, name)
	if initialCommand != "" {
		log.Printf("Session %s initial command: %s", id, initialCommand)
	}
	return session, nil
}

func (sm *SessionManager) GetSession(id string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, exists := sm.sessions[id]
	return session, exists
}

func (sm *SessionManager) ListSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	sessions := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

func (sm *SessionManager) RemoveSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if session, exists := sm.sessions[id]; exists {
		session.terminate()
		delete(sm.sessions, id)
		log.Printf("Removed session %s", id)
	}
}

func (s *Session) startShell() error {
	s.Command = exec.Command("/bin/bash")
	s.Command.Env = os.Environ()

	var err error
	s.PTY, err = pty.Start(s.Command)
	if err != nil {
		return err
	}

	// Monitor the process
	go s.monitor()

	return nil
}

func (s *Session) monitor() {
	defer func() {
		s.terminate()
	}()

	// Wait for the command to finish
	if s.Command.Process != nil {
		s.Command.Process.Wait()
	}
}

func (s *Session) terminate() {
	s.cancel()
	
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all client connections
	for clientID := range s.clients {
		delete(s.clients, clientID)
	}

	// Close PTY
	if s.PTY != nil {
		s.PTY.Close()
	}

	// Kill the process
	if s.Command != nil && s.Command.Process != nil {
		s.Command.Process.Kill()
	}
}

func (s *Session) AddClient(clientID string, writer io.Writer, reader io.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client := &Client{
		ID:     clientID,
		Writer: writer,
		Reader: reader,
	}

	s.clients[clientID] = client
	log.Printf("Client %s attached to session %s", clientID, s.ID)

	// Start copying data between client and PTY
	go s.handleClient(client)
}

func (s *Session) RemoveClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.clients[clientID]; exists {
		delete(s.clients, clientID)
		log.Printf("Client %s detached from session %s", clientID, s.ID)
	}
}

func (s *Session) handleClient(client *Client) {
	defer s.RemoveClient(client.ID)

	// Copy from PTY to client
	go func() {
		io.Copy(client.Writer, s.PTY)
	}()

	// Copy from client to PTY
	io.Copy(s.PTY, client.Reader)
}

func (s *Session) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Session) Resize(rows, cols int) error {
	if s.PTY == nil {
		return fmt.Errorf("no PTY available")
	}
	return pty.Setsize(s.PTY, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
}

func (s *Session) GetPTY() pty.Pty {
	return s.PTY
}

func (s *Session) GetPTYSession() *client.PTYSession {
	return client.NewPTYSession(s.PTY)
}