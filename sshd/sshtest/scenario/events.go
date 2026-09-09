// Package scenario provides data types and YAML parsing for SSH test scenarios.
package scenario

import (
	"fmt"
	"strings"
	"time"
)

// Predefined event IDs.
const (
	EventConnected        = "connected"
	EventDisconnected     = "disconnected"
	EventAuthSuccess      = "auth.success"
	EventAuthFailure      = "auth.failure"
	EventSessionStarted   = "session.started"
	EventSessionEnded     = "session.ended"
	EventExecStarted      = "exec.started"
	EventExecCompleted    = "exec.completed"
	EventPTYRequested     = "pty.requested"
	EventPTYResized       = "pty.resized"
	EventSFTPStarted      = "sftp.started"
	EventSFTPEnded        = "sftp.ended"
	EventForwardRequested = "forward.requested"
	EventForwardCancelled = "forward.cancelled"
	EventShellStarted     = "shell.started"
	EventShellEnded       = "shell.ended"
)

// Event represents something that happened during the test.
type Event struct {
	ID        string
	Timestamp time.Time
	Attrs     map[string]string
}

// Matches checks if this event matches the given ID and key-value pairs.
func (e Event) Matches(id string, attrs ...string) bool {
	if e.ID != id {
		return false
	}
	if len(attrs)%2 != 0 {
		return false
	}
	for i := 0; i < len(attrs); i += 2 {
		key, value := attrs[i], attrs[i+1]
		if e.Attrs[key] != value {
			return false
		}
	}
	return true
}

// String returns a human-readable representation of the event.
func (e Event) String() string {
	var sb strings.Builder
	sb.WriteString(e.ID)
	if len(e.Attrs) > 0 {
		sb.WriteString("{")
		first := true
		for k, v := range e.Attrs {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(v)
		}
		sb.WriteString("}")
	}
	return sb.String()
}

// ScenarioError provides detailed error context for scenario failures.
type ScenarioError struct {
	Scenario    string
	StepNum     int
	ClientName  string
	Action      string
	Expectation string
	Err         error
}

func (e *ScenarioError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("scenario %q failed at step %d", e.Scenario, e.StepNum+1))
	if e.ClientName != "" {
		sb.WriteString(fmt.Sprintf(" (client: %s)", e.ClientName))
	}
	if e.Action != "" {
		sb.WriteString(fmt.Sprintf("\n  action: %s", e.Action))
	}
	if e.Expectation != "" {
		sb.WriteString(fmt.Sprintf("\n  expectation: %s", e.Expectation))
	}
	sb.WriteString(fmt.Sprintf("\n  error: %s", e.Err))
	return sb.String()
}

func (e *ScenarioError) Unwrap() error {
	return e.Err
}
