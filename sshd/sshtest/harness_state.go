package sshtest

import (
	"context"
	"io"

	"github.com/jpillora/sshd-lite/sshd/sshtest/log"
)

func (e *Environment) clientByName(name string) Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clients[name]
}

func (e *Environment) firstClientName() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for name := range e.clients {
		return name
	}
	return ""
}

func (e *Environment) sessionByName(name string) Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions[name]
}

func (e *Environment) storeExecResult(result *ExecResult) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.stopping || e.stopped {
		return false
	}
	if result == nil {
		e.lastExecResult = nil
		return true
	}
	copy := *result
	e.lastExecResult = &copy
	return true
}

func (e *Environment) execResult() (*ExecResult, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastExecResult == nil {
		return nil, false
	}
	copy := *e.lastExecResult
	return &copy, true
}

func (e *Environment) outputState(name string) (Session, *ExecResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := e.lastExecResult
	if result != nil {
		copy := *result
		result = &copy
	}
	return e.sessions[name], result
}

func (e *Environment) storeSession(name string, session Session) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.stopping || e.stopped {
		return false
	}
	e.sessions[name] = session
	return true
}

func (e *Environment) takeSession(name string) Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	session := e.sessions[name]
	delete(e.sessions, name)
	return session
}

func (e *Environment) storeForwardListener(name string, listener io.Closer) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.stopping || e.stopped {
		return false
	}
	e.forwardListeners[name] = append(e.forwardListeners[name], listener)
	return true
}

// Server returns the server instance.
func (e *Environment) Server() Server {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.server
}

// Client returns a client by name.
func (e *Environment) Client(name string) Client {
	client := e.clientByName(name)
	if client == nil {
		e.t.Fatalf("client %q not found", name)
		return nil
	}
	return client
}

// Clients returns all clients.
func (e *Environment) Clients() map[string]Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	clients := make(map[string]Client, len(e.clients))
	for name, client := range e.clients {
		clients[name] = client
	}
	return clients
}

// Events returns the shared event bus.
func (e *Environment) Events() *EventBus {
	return e.events
}

// Logs returns the shared log capture.
func (e *Environment) Logs() *log.Capture {
	return e.logs
}

// Context returns the environment's context.
func (e *Environment) Context() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ctx
}
