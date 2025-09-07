package smux

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

type sessionManager struct {
	sessions map[string]*session
	mu       sync.RWMutex
	logger   *slog.Logger
}

func newSessionManager(logger *slog.Logger) *sessionManager {
	return &sessionManager{
		sessions: make(map[string]*session),
		logger:   logger,
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
	sm.logger.Info("Created session", "id", id)
	if initialCommand != "" {
		sm.logger.Debug("Session initial command", "id", id, "command", initialCommand)
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
		sm.logger.Info("Removed session", "id", id)
	}
}