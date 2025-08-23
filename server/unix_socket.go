package sshd

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

type SessionInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartTime time.Time `json:"start_time"`
	PID       int       `json:"pid"`
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionInfo
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionInfo),
	}
}

func (sm *SessionManager) AddSession(id, name string, pid int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[id] = &SessionInfo{
		ID:        id,
		Name:      name,
		StartTime: time.Now(),
		PID:       pid,
	}
}

func (sm *SessionManager) RemoveSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, id)
}

func (sm *SessionManager) ListSessions() []SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	sessions := make([]SessionInfo, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, *session)
	}
	return sessions
}

func (sm *SessionManager) GetSessionsJSON() ([]byte, error) {
	sessions := sm.ListSessions()
	return json.Marshal(sessions)
}

func (s *Server) StartUnixSocket(socketPath string) error {
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	return s.StartWith(l)
}