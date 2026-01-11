package sshtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

// EventBus collects and queries events.
type EventBus struct {
	events   []scenario.Event
	mu       sync.RWMutex
	notifyCh chan struct{}
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		events:   make([]scenario.Event, 0),
		notifyCh: make(chan struct{}, 100),
	}
}

// Emit sends an event. Attrs should be key-value pairs.
func (eb *EventBus) Emit(id string, attrs ...string) {
	event := scenario.Event{
		ID:        id,
		Timestamp: time.Now(),
		Attrs:     make(map[string]string),
	}

	// Parse key-value pairs
	for i := 0; i+1 < len(attrs); i += 2 {
		event.Attrs[attrs[i]] = attrs[i+1]
	}

	eb.mu.Lock()
	eb.events = append(eb.events, event)
	eb.mu.Unlock()

	// Notify waiters (non-blocking)
	select {
	case eb.notifyCh <- struct{}{}:
	default:
	}
}

// EmitEvent emits a pre-constructed event.
func (eb *EventBus) EmitEvent(event scenario.Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Attrs == nil {
		event.Attrs = make(map[string]string)
	}

	eb.mu.Lock()
	eb.events = append(eb.events, event)
	eb.mu.Unlock()

	select {
	case eb.notifyCh <- struct{}{}:
	default:
	}
}

// Wait blocks until an event matching the criteria is received.
// Uses the default timeout of 10 seconds.
func (eb *EventBus) Wait(id string, attrs ...string) (scenario.Event, error) {
	return eb.WaitTimeout(10*time.Second, id, attrs...)
}

// WaitTimeout waits with a specific timeout.
func (eb *EventBus) WaitTimeout(timeout time.Duration, id string, attrs ...string) (scenario.Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return eb.WaitContext(ctx, id, attrs...)
}

// WaitContext waits with a context for cancellation.
func (eb *EventBus) WaitContext(ctx context.Context, id string, attrs ...string) (scenario.Event, error) {
	// First check existing events
	if event, found := eb.Find(id, attrs...); found {
		return event, nil
	}

	// Wait for new events
	for {
		select {
		case <-ctx.Done():
			return scenario.Event{}, fmt.Errorf("timeout waiting for event %q: %w", id, ctx.Err())
		case <-eb.notifyCh:
			if event, found := eb.Find(id, attrs...); found {
				return event, nil
			}
		}
	}
}

// Find returns the first event matching the criteria.
func (eb *EventBus) Find(id string, attrs ...string) (scenario.Event, bool) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, event := range eb.events {
		if event.Matches(id, attrs...) {
			return event, true
		}
	}
	return scenario.Event{}, false
}

// FindAll returns all events matching the criteria.
func (eb *EventBus) FindAll(id string, attrs ...string) []scenario.Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var result []scenario.Event
	for _, event := range eb.events {
		if event.Matches(id, attrs...) {
			result = append(result, event)
		}
	}
	return result
}

// All returns all events.
func (eb *EventBus) All() []scenario.Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	result := make([]scenario.Event, len(eb.events))
	copy(result, eb.events)
	return result
}

// Clear removes all events.
func (eb *EventBus) Clear() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.events = eb.events[:0]
}

// Count returns the number of events.
func (eb *EventBus) Count() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.events)
}

// Has checks if an event matching the criteria exists.
func (eb *EventBus) Has(id string, attrs ...string) bool {
	_, found := eb.Find(id, attrs...)
	return found
}
