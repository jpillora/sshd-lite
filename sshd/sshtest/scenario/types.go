// Package scenario provides data types and YAML parsing for SSH test scenarios.
package scenario

import (
	"time"
)

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
	ActionSFTPUpload    ActionType = "sftp_upload"
	ActionSFTPDownload  ActionType = "sftp_download"
)

// ExpectationSpec describes an expectation to verify.
type ExpectationSpec struct {
	Type   ExpectationType
	Params map[string]interface{}
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

// LocalPath returns the local filesystem path for SFTP actions.
func (a *ActionSpec) LocalPath() string {
	return a.LocalAddr()
}

// RemotePath returns the remote filesystem path for SFTP actions.
func (a *ActionSpec) RemotePath() string {
	return a.RemoteAddr()
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
