package smux

import (
	"fmt"
	"log"
	"sync"
)

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

	session := newSession(id, name)

	// Start the shell process
	if err := session.startShell(); err != nil {
		session.cancel()
		return nil, fmt.Errorf("failed to start shell: %v", err)
	}

	// If there's an initial command, send it to the shell
	if initialCommand != "" {
		session.executeInitialCommand(initialCommand)
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