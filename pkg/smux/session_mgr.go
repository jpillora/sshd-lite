package smux

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"
)

type sessionManager struct {
	sessions map[string]*session
	mu       sync.RWMutex
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		sessions: make(map[string]*session),
	}
}

func (sm *sessionManager) generateSessionID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)[:8]
}

func (sm *sessionManager) nextAvailableID() string {
	for i := 1; ; i++ {
		id := strconv.Itoa(i)
		if _, exists := sm.sessions[id]; !exists {
			return id
		}
	}
}

func (sm *sessionManager) CreateSession(id string) (*session, error) {
	return sm.CreateSessionWithCommand(id, "")
}

func (sm *sessionManager) CreateSessionWithCommand(id, initialCommand string) (*session, error) {
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

func (sm *sessionManager) GetSession(id string) (*session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, exists := sm.sessions[id]
	return session, exists
}

func (sm *sessionManager) ListSessions() []*session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	sessions := make([]*session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

func (sm *sessionManager) RemoveSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if session, exists := sm.sessions[id]; exists {
		session.terminate()
		delete(sm.sessions, id)
		log.Printf("Removed session %s", id)
	}
}