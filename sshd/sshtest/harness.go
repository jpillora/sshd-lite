// Package sshtest provides an AI-friendly test harness for SSH servers and clients.
//
// The harness provides a fluent API for setting up test environments:
//
//	env := sshtest.New(t).
//		WithServer(sshtest.ServerWithSFTP(true)).
//		WithClient("alice", sshtest.ClientWithKeySeed("alice")).
//		Start()
//	defer env.Stop()
//
//	result, err := env.Client("alice").Exec("echo hello")
package sshtest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/sshd/sshtest/log"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

// Environment manages the test lifecycle for SSH server/client testing.
type Environment struct {
	t      testing.TB
	ctx    context.Context
	cancel context.CancelFunc

	server        Server
	serverOpts    []ServerOption
	clients       map[string]Client
	clientConfigs map[string][]ClientOption
	events        *EventBus
	logs          *log.Capture

	// State for actions and expectations
	sessions         map[string]Session
	lastExecResult   *ExecResult
	forwardListeners map[string][]interface{}

	keySeed string
	timeout time.Duration
	started bool
	mu      sync.Mutex
}

// New creates a new test environment.
func New(t testing.TB) *Environment {
	ctx, cancel := context.WithCancel(context.Background())
	return &Environment{
		t:             t,
		ctx:           ctx,
		cancel:        cancel,
		clients:       make(map[string]Client),
		clientConfigs: make(map[string][]ClientOption),
		sessions:      make(map[string]Session),
		events:        NewEventBus(),
		logs:          log.NewCapture(),
		keySeed:       "test",
		timeout:       30 * time.Second,
	}
}

// WithServer configures the server with the given options.
func (e *Environment) WithServer(opts ...ServerOption) *Environment {
	e.serverOpts = append(e.serverOpts, opts...)
	return e
}

// WithClient adds a client configuration with the given name.
func (e *Environment) WithClient(name string, opts ...ClientOption) *Environment {
	e.clientConfigs[name] = opts
	return e
}

// WithKeySeed sets the base seed for deterministic key generation.
func (e *Environment) WithKeySeed(seed string) *Environment {
	e.keySeed = seed
	return e
}

// WithTimeout sets the default timeout for operations.
func (e *Environment) WithTimeout(d time.Duration) *Environment {
	e.timeout = d
	return e
}

// Start initializes and starts the server and creates clients.
func (e *Environment) Start() *Environment {
	e.t.Helper()

	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		e.t.Fatal("environment already started")
		return e
	}
	e.started = true
	e.mu.Unlock()

	// Add shared events and logs to server options
	opts := append([]ServerOption{
		ServerWithEvents(e.events),
		ServerWithLogger(e.logs),
	}, e.serverOpts...)

	// Create and start server
	server, err := NewServer(opts...)
	if err != nil {
		e.t.Fatalf("failed to create server: %v", err)
		return e
	}
	e.server = server

	// Add authorized keys for each client with key-based auth
	for name, clientOpts := range e.clientConfigs {
		// Check if client uses key auth
		cfg := defaultClientConfig()
		for _, opt := range clientOpts {
			opt(cfg)
		}

		// If client has key seed, add authorized key
		if cfg.keySeed != "" {
			pubKey, err := key.PublicKeyFromSeed(cfg.keySeed)
			if err != nil {
				e.t.Fatalf("failed to generate public key for client %s: %v", name, err)
				return e
			}
			server.AddAuthorizedKey(name, pubKey)
		}
	}

	// Start server
	if err := server.Start(e.ctx); err != nil {
		e.t.Fatalf("failed to start server: %v", err)
		return e
	}

	// Give server time to be ready
	time.Sleep(50 * time.Millisecond)

	// Create clients
	for name, clientOpts := range e.clientConfigs {
		opts := append([]ClientOption{
			ClientWithName(name),
			ClientWithEvents(e.events),
		}, clientOpts...)

		client, err := NewClient(server.Addr(), opts...)
		if err != nil {
			e.t.Fatalf("failed to create client %s: %v", name, err)
			return e
		}
		e.clients[name] = client
	}

	return e
}

// Stop stops the server and closes all clients.
func (e *Environment) Stop() {
	e.t.Helper()

	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	// Close all clients
	for name, client := range e.clients {
		if err := client.Close(); err != nil {
			e.t.Logf("warning: failed to close client %s: %v", name, err)
		}
	}

	// Stop server
	if e.server != nil {
		if err := e.server.Stop(); err != nil {
			e.t.Logf("warning: failed to stop server: %v", err)
		}
	}

	// Cancel context
	if e.cancel != nil {
		e.cancel()
	}
}

// Server returns the server instance.
func (e *Environment) Server() Server {
	return e.server
}

// Client returns a client by name.
func (e *Environment) Client(name string) Client {
	client, ok := e.clients[name]
	if !ok {
		e.t.Fatalf("client %q not found", name)
		return nil
	}
	return client
}

// Clients returns all clients.
func (e *Environment) Clients() map[string]Client {
	return e.clients
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
	return e.ctx
}

// Run executes a scenario against this environment.
func (e *Environment) Run(sc *scenario.Scenario) error {
	e.t.Helper()

	if sc == nil {
		return fmt.Errorf("scenario is nil")
	}

	runner := &Runner{
		env:     e,
		timeout: e.timeout,
	}

	return runner.Run(e.ctx, sc)
}

// RunYAML parses and executes a YAML scenario.
func (e *Environment) RunYAML(yaml string) error {
	e.t.Helper()

	sc, err := scenario.Parse(yaml)
	if err != nil {
		return fmt.Errorf("failed to parse YAML scenario: %w", err)
	}

	return e.Run(sc)
}

// MustRun executes a scenario and fails the test on error.
func (e *Environment) MustRun(sc *scenario.Scenario) {
	e.t.Helper()
	if err := e.Run(sc); err != nil {
		e.t.Fatalf("scenario %q failed: %v", sc.Name, err)
	}
}

// MustRunYAML parses and executes a YAML scenario, failing on error.
func (e *Environment) MustRunYAML(yaml string) {
	e.t.Helper()
	if err := e.RunYAML(yaml); err != nil {
		e.t.Fatalf("YAML scenario failed: %v", err)
	}
}

// T returns the testing.TB instance.
func (e *Environment) T() testing.TB {
	return e.t
}

// Require is a helper for common assertions that fail the test.
type Require struct {
	t testing.TB
}

// Env returns a Require helper for assertions.
func (e *Environment) Require() *Require {
	return &Require{t: e.t}
}

// NoError fails the test if err is not nil.
func (r *Require) NoError(err error, msgAndArgs ...interface{}) {
	r.t.Helper()
	if err != nil {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: %v", fmt.Sprint(msgAndArgs...), err)
		} else {
			r.t.Fatalf("unexpected error: %v", err)
		}
	}
}

// Equal fails the test if expected != actual.
func (r *Require) Equal(expected, actual interface{}, msgAndArgs ...interface{}) {
	r.t.Helper()
	if expected != actual {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: expected %v, got %v", fmt.Sprint(msgAndArgs...), expected, actual)
		} else {
			r.t.Fatalf("expected %v, got %v", expected, actual)
		}
	}
}

// Contains fails the test if s does not contain substr.
func (r *Require) Contains(s, substr string, msgAndArgs ...interface{}) {
	r.t.Helper()
	if len(s) == 0 || len(substr) == 0 {
		r.t.Fatalf("contains check failed: s=%q, substr=%q", s, substr)
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	if len(msgAndArgs) > 0 {
		r.t.Fatalf("%s: %q does not contain %q", fmt.Sprint(msgAndArgs...), s, substr)
	} else {
		r.t.Fatalf("%q does not contain %q", s, substr)
	}
}

// True fails the test if condition is false.
func (r *Require) True(condition bool, msgAndArgs ...interface{}) {
	r.t.Helper()
	if !condition {
		if len(msgAndArgs) > 0 {
			r.t.Fatalf("%s: expected true", fmt.Sprint(msgAndArgs...))
		} else {
			r.t.Fatal("expected true")
		}
	}
}
