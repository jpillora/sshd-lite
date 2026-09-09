package sshtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	forwardListeners map[string][]io.Closer

	keySeed  string
	timeout  time.Duration
	started  bool
	stopping bool
	stopped  bool
	stopDone chan struct{}
	mu       sync.Mutex
}

// New creates a new test environment.
func New(t testing.TB) *Environment {
	ctx, cancel := context.WithCancel(context.Background())
	return &Environment{
		t:                t,
		ctx:              ctx,
		cancel:           cancel,
		clients:          make(map[string]Client),
		clientConfigs:    make(map[string][]ClientOption),
		sessions:         make(map[string]Session),
		forwardListeners: make(map[string][]io.Closer),
		events:           NewEventBus(),
		logs:             log.NewCapture(),
		keySeed:          "test",
		timeout:          30 * time.Second,
	}
}

// WithServer configures the server with the given options.
func (e *Environment) WithServer(opts ...ServerOption) *Environment {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.stopping || e.stopped {
		e.t.Fatal("cannot configure server after environment lifecycle has started")
	}
	e.serverOpts = append(e.serverOpts, opts...)
	return e
}

// WithClient adds a client configuration with the given name.
func (e *Environment) WithClient(name string, opts ...ClientOption) *Environment {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.stopping || e.stopped {
		e.t.Fatal("cannot configure clients after environment lifecycle has started")
	}
	e.clientConfigs[name] = opts
	return e
}

// WithKeySeed sets the base seed for deterministic key generation.
func (e *Environment) WithKeySeed(seed string) *Environment {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.stopping || e.stopped {
		e.t.Fatal("cannot configure key seed after environment lifecycle has started")
	}
	e.keySeed = seed
	return e
}

// WithTimeout sets the default timeout for operations.
func (e *Environment) WithTimeout(d time.Duration) *Environment {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.stopping || e.stopped {
		e.t.Fatal("cannot configure timeout after environment lifecycle has started")
	}
	e.timeout = d
	return e
}

// Start initializes and starts the server and creates clients.
func (e *Environment) Start() *Environment {
	e.t.Helper()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		e.t.Fatal("environment already started")
		return e
	}
	if e.stopping || e.stopped {
		e.t.Fatal("environment cannot be started after Stop")
		return e
	}

	// Add shared events and logs to server options
	opts := append([]ServerOption{
		ServerWithEvents(e.events),
		ServerWithLogger(e.logs),
	}, e.serverOpts...)

	// Create and start server
	server, err := NewServer(opts...)
	if err != nil {
		e.stopped = true
		e.cancel()
		e.t.Fatalf("failed to create server: %v", err)
		return e
	}

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
				e.stopped = true
				e.cancel()
				e.t.Fatalf("failed to generate public key for client %s: %v", name, err)
				return e
			}
			server.AddAuthorizedKey(name, pubKey)
		}
	}

	// Start server
	if err := server.Start(e.ctx); err != nil {
		e.stopped = true
		e.cancel()
		e.t.Fatalf("failed to start server: %v", err)
		return e
	}

	// Create clients
	clients := make(map[string]Client, len(e.clientConfigs))
	for name, clientOpts := range e.clientConfigs {
		opts := append([]ClientOption{
			ClientWithName(name),
			ClientWithEvents(e.events),
		}, clientOpts...)

		client, err := NewClient(server.Addr(), opts...)
		if err != nil {
			for _, created := range clients {
				_ = created.Close()
			}
			_ = server.Stop()
			e.stopped = true
			e.cancel()
			e.t.Fatalf("failed to create client %s: %v", name, err)
			return e
		}
		clients[name] = client
	}

	// Publish a fully initialized environment in one critical section. Stop and
	// all accessors either see nothing started or this complete snapshot.
	e.server = server
	e.clients = clients
	e.started = true
	return e
}

// Stop stops the server and closes all clients.
func (e *Environment) Stop() {
	e.t.Helper()

	e.mu.Lock()
	if e.stopping {
		done := e.stopDone
		e.mu.Unlock()
		<-done
		return
	}
	if e.stopped {
		e.mu.Unlock()
		return
	}
	if !e.started {
		e.stopped = true
		e.cancel()
		e.mu.Unlock()
		return
	}
	e.stopping = true
	e.stopDone = make(chan struct{})
	done := e.stopDone
	sessions := e.sessions
	e.sessions = make(map[string]Session)
	e.lastExecResult = nil
	forwardListeners := e.forwardListeners
	e.forwardListeners = make(map[string][]io.Closer)
	clients := make(map[string]Client, len(e.clients))
	for name, client := range e.clients {
		clients[name] = client
	}
	server := e.server
	e.mu.Unlock()

	// Cancel action contexts first, then close resources from the inside out so
	// blocked session and forwarding operations are interrupted before the SSH
	// transports and server disappear.
	if e.cancel != nil {
		e.cancel()
	}
	for name, session := range sessions {
		if err := session.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			e.t.Logf("warning: failed to close session for client %s: %v", name, err)
		}
	}
	for name, listeners := range forwardListeners {
		for _, listener := range listeners {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				e.t.Logf("warning: failed to close forwarding listener for client %s: %v", name, err)
			}
		}
	}
	for name, client := range clients {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			e.t.Logf("warning: failed to close client %s: %v", name, err)
		}
	}

	if server != nil {
		if err := server.Stop(); err != nil && !errors.Is(err, net.ErrClosed) {
			e.t.Logf("warning: failed to stop server: %v", err)
		}
	}

	e.mu.Lock()
	e.stopping = false
	e.stopped = true
	close(done)
	e.mu.Unlock()
}

// Run executes a scenario against this environment.
func (e *Environment) Run(sc *scenario.Scenario) error {
	e.t.Helper()

	if sc == nil {
		return fmt.Errorf("scenario is nil")
	}

	e.mu.Lock()
	ctx := e.ctx
	timeout := e.timeout
	e.mu.Unlock()
	runner := &Runner{
		env:     e,
		timeout: timeout,
	}

	return runner.Run(ctx, sc)
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
