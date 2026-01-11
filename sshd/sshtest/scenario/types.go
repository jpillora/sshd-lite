// Package scenario provides data types and YAML parsing for SSH test scenarios.
package scenario

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Action is something a client does in a test scenario.
type Action interface {
	Execute(ctx context.Context, env interface{}, clientName string) error
	String() string
}

// Expectation is something we verify after actions.
type Expectation interface {
	Check(ctx context.Context, env interface{}, clientName string) error
	String() string
}

// Scenario describes a test scenario.
type Scenario struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Steps       []Step `yaml:"steps"`
}

// Step is a single step in a scenario.
type Step struct {
	Client  string            `yaml:"client"`
	Actions []ActionSpec      `yaml:"actions"`
	Expect  []ExpectationSpec `yaml:"expect"`
}

// ActionSpec describes an action to perform (parsed from YAML).
type ActionSpec struct {
	Type   ActionType
	Params map[string]interface{}
}

func (a *ActionSpec) Execute(ctx context.Context, env interface{}, clientName string) error {
	return nil
}

func (a *ActionSpec) String() string {
	return string(a.Type)
}

// ActionType identifies the type of action.
type ActionType string

const (
	ActionConnect       ActionType = "connect"
	ActionDisconnect    ActionType = "disconnect"
	ActionShell         ActionType = "shell"
	ActionCloseShell    ActionType = "close_shell"
	ActionExec          ActionType = "exec"
	ActionInput         ActionType = "input"
	ActionLine          ActionType = "line"
	ActionKey           ActionType = "key"
	ActionSleep         ActionType = "sleep"
	ActionResize        ActionType = "resize"
	ActionWaitForEvent  ActionType = "wait_for_event"
	ActionLocalForward  ActionType = "local_forward"
	ActionRemoteForward ActionType = "remote_forward"
)

// ExpectationSpec describes an expectation to verify.
type ExpectationSpec struct {
	Type   ExpectationType
	Params map[string]interface{}
}

func (e *ExpectationSpec) Check(ctx context.Context, env interface{}, clientName string) error {
	return nil
}

func (e *ExpectationSpec) String() string {
	return string(e.Type)
}

// ExpectationType identifies the type of expectation.
type ExpectationType string

const (
	ExpectConnected     ExpectationType = "connected"
	ExpectDisconnected  ExpectationType = "disconnected"
	ExpectOutput        ExpectationType = "output"
	ExpectOutputMatch   ExpectationType = "output_match"
	ExpectStdout        ExpectationType = "stdout"
	ExpectStderr        ExpectationType = "stderr"
	ExpectExitCode      ExpectationType = "exit_code"
	ExpectScreen        ExpectationType = "screen"
	ExpectEvent         ExpectationType = "event"
	ExpectNoEvent       ExpectationType = "no_event"
	ExpectWaitForOutput ExpectationType = "wait_for_output"
)

// Helper methods for ActionSpec params.

// Command returns the command for exec actions.
func (a *ActionSpec) Command() string {
	if v, ok := a.Params["command"].(string); ok {
		return v
	}
	return ""
}

// Text returns the text for input/line actions.
func (a *ActionSpec) Text() string {
	if v, ok := a.Params["text"].(string); ok {
		return v
	}
	return ""
}

// KeyName returns the key name for key actions.
func (a *ActionSpec) KeyName() string {
	if v, ok := a.Params["key"].(string); ok {
		return v
	}
	return ""
}

// Duration returns the duration for sleep actions.
func (a *ActionSpec) Duration() time.Duration {
	if v, ok := a.Params["duration"].(time.Duration); ok {
		return v
	}
	return 0
}

// Cols returns cols for resize actions.
func (a *ActionSpec) Cols() uint32 {
	if v, ok := a.Params["cols"].(int); ok {
		return uint32(v)
	}
	if v, ok := a.Params["cols"].(float64); ok {
		return uint32(v)
	}
	return 0
}

// Rows returns rows for resize actions.
func (a *ActionSpec) Rows() uint32 {
	if v, ok := a.Params["rows"].(int); ok {
		return uint32(v)
	}
	if v, ok := a.Params["rows"].(float64); ok {
		return uint32(v)
	}
	return 0
}

// EventID returns the event ID for wait_for_event actions.
func (a *ActionSpec) EventID() string {
	if v, ok := a.Params["event"].(string); ok {
		return v
	}
	return ""
}

// Attrs returns attributes for event actions.
func (a *ActionSpec) Attrs() []string {
	if v, ok := a.Params["attrs"].([]string); ok {
		return v
	}
	return nil
}

// Timeout returns the timeout duration.
func (a *ActionSpec) Timeout() time.Duration {
	if v, ok := a.Params["timeout"].(time.Duration); ok {
		return v
	}
	return 10 * time.Second
}

// LocalAddr returns local address for forwarding.
func (a *ActionSpec) LocalAddr() string {
	if v, ok := a.Params["local"].(string); ok {
		return v
	}
	return ""
}

// RemoteAddr returns remote address for forwarding.
func (a *ActionSpec) RemoteAddr() string {
	if v, ok := a.Params["remote"].(string); ok {
		return v
	}
	return ""
}

// Helper methods for ExpectationSpec params.

// Contains returns the text to check for output expectations.
func (e *ExpectationSpec) Contains() string {
	if v, ok := e.Params["contains"].(string); ok {
		return v
	}
	return ""
}

// Pattern returns the regex pattern for output_match expectations.
func (e *ExpectationSpec) Pattern() string {
	if v, ok := e.Params["pattern"].(string); ok {
		return v
	}
	return ""
}

// Code returns the exit code for exit_code expectations.
func (e *ExpectationSpec) Code() int {
	if v, ok := e.Params["code"].(int); ok {
		return v
	}
	return 0
}

// EventID returns the event ID for event expectations.
func (e *ExpectationSpec) EventID() string {
	if v, ok := e.Params["event"].(string); ok {
		return v
	}
	return ""
}

// Attrs returns attributes for event expectations.
func (e *ExpectationSpec) Attrs() []string {
	if v, ok := e.Params["attrs"].([]string); ok {
		return v
	}
	return nil
}

// Text returns text for wait_for_output expectations.
func (e *ExpectationSpec) Text() string {
	if v, ok := e.Params["text"].(string); ok {
		return v
	}
	return ""
}

// Timeout returns timeout for wait_for_output expectations.
func (e *ExpectationSpec) Timeout() time.Duration {
	if v, ok := e.Params["timeout"].(time.Duration); ok {
		return v
	}
	return 5 * time.Second
}

// Key represents special keys for SendKey action.
type Key string

const (
	KeyEnter     Key = "\r"
	KeyTab       Key = "\t"
	KeyEscape    Key = "\x1b"
	KeyBackspace Key = "\x7f"
	KeyCtrlC     Key = "\x03"
	KeyCtrlD     Key = "\x04"
	KeyCtrlZ     Key = "\x1a"
	KeyUp        Key = "\x1b[A"
	KeyDown      Key = "\x1b[B"
	KeyRight     Key = "\x1b[C"
	KeyLeft      Key = "\x1b[D"
)

// ParseKey converts a key name string to Key constant.
func ParseKey(name string) (Key, error) {
	switch strings.ToLower(name) {
	case "enter", "return":
		return KeyEnter, nil
	case "tab":
		return KeyTab, nil
	case "escape", "esc":
		return KeyEscape, nil
	case "backspace":
		return KeyBackspace, nil
	case "ctrl+c", "ctrlc":
		return KeyCtrlC, nil
	case "ctrl+d", "ctrld":
		return KeyCtrlD, nil
	case "ctrl+z", "ctrlz":
		return KeyCtrlZ, nil
	case "up":
		return KeyUp, nil
	case "down":
		return KeyDown, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
	default:
		return "", fmt.Errorf("unknown key: %s", name)
	}
}

// KeyLabel returns the human-readable name for a key.
func KeyLabel(k Key) string {
	switch k {
	case KeyEnter:
		return "Enter"
	case KeyTab:
		return "Tab"
	case KeyEscape:
		return "Escape"
	case KeyBackspace:
		return "Backspace"
	case KeyCtrlC:
		return "Ctrl+C"
	case KeyCtrlD:
		return "Ctrl+D"
	case KeyCtrlZ:
		return "Ctrl+Z"
	case KeyUp:
		return "Up"
	case KeyDown:
		return "Down"
	case KeyLeft:
		return "Left"
	case KeyRight:
		return "Right"
	default:
		return "unknown"
	}
}

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
