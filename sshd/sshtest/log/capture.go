// Package log provides slog capture utilities for testing.
package log

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry represents a captured log entry.
type Entry struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
	Attrs     map[string]any
}

// String returns a formatted log entry.
func (e Entry) String() string {
	var sb strings.Builder
	sb.WriteString(e.Timestamp.Format("15:04:05.000"))
	sb.WriteString(" ")
	sb.WriteString(e.Level.String())
	sb.WriteString(" ")
	sb.WriteString(e.Message)
	if len(e.Attrs) > 0 {
		for k, v := range e.Attrs {
			sb.WriteString(" ")
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(fmt.Sprint(v))
		}
	}
	return sb.String()
}

// Matches checks if the entry matches the given level and contains the text.
func (e Entry) Matches(level slog.Level, contains string) bool {
	return e.Level == level && strings.Contains(e.Message, contains)
}

// Contains checks if the entry message contains the text.
func (e Entry) Contains(text string) bool {
	return strings.Contains(e.Message, text)
}

// Capture collects log output for assertions.
type Capture struct {
	entries []Entry
	mu      sync.RWMutex
}

// NewCapture creates a new log capture.
func NewCapture() *Capture {
	return &Capture{
		entries: make([]Entry, 0),
	}
}

// Add adds a log entry.
func (c *Capture) Add(entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	c.entries = append(c.entries, entry)
}

// Assert checks that a log message containing the text exists.
func (c *Capture) Assert(contains string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.entries {
		if entry.Contains(contains) {
			return nil
		}
	}
	return fmt.Errorf("no log entry containing %q found in %d entries", contains, len(c.entries))
}

// AssertLevel checks for a log at a specific level containing the text.
func (c *Capture) AssertLevel(level slog.Level, contains string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.entries {
		if entry.Matches(level, contains) {
			return nil
		}
	}
	return fmt.Errorf("no %s log entry containing %q found", level, contains)
}

// Find returns the first entry containing the text.
func (c *Capture) Find(contains string) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.entries {
		if entry.Contains(contains) {
			return entry, true
		}
	}
	return Entry{}, false
}

// FindAll returns all entries containing the text.
func (c *Capture) FindAll(contains string) []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []Entry
	for _, entry := range c.entries {
		if entry.Contains(contains) {
			result = append(result, entry)
		}
	}
	return result
}

// All returns all log entries.
func (c *Capture) All() []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]Entry, len(c.entries))
	copy(result, c.entries)
	return result
}

// Clear removes all entries.
func (c *Capture) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = c.entries[:0]
}

// String returns all logs as a formatted string.
func (c *Capture) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder
	for _, entry := range c.entries {
		sb.WriteString(entry.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// Count returns the number of entries.
func (c *Capture) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Logger returns an slog.Logger that writes to this capture.
func (c *Capture) Logger() *slog.Logger {
	return slog.New(NewHandler(c))
}
