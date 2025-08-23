package smux

import (
	"fmt"
	"log"
	"strconv"
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

func (sm *SessionManager) nextAvailableID() string {
	for i := 1; ; i++ {
		id := strconv.Itoa(i)
		if _, exists := sm.sessions[id]; !exists {
			return id
		}
	}
}

func (sm *SessionManager) CreateSession(id string) (*Session, error) {
	return sm.CreateSessionWithCommand(id, "")
}

func (sm *SessionManager) CreateSessionWithCommand(id, initialCommand string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if id == "" {
		id = sm.nextAvailableID()
	}

	if _, exists := sm.sessions[id]; exists {
		return nil, fmt.Errorf("session %s already exists", id)
	}

	session := newSession(id)

	if err := session.startShell(); err != nil {
		session.cancel()
		return nil, fmt.Errorf("failed to start shell: %v", err)
	}

	if initialCommand != "" {
		session.executeInitialCommand(initialCommand)
	}

	sm.sessions[id] = session
	log.Printf("Created session %s", id)
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