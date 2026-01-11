package sshtest

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

// expectOutputAction checks that output contains text.
type expectOutputAction struct {
	contains string
}

// ExpectOutput returns an expectation that output contains text.
func ExpectOutput(contains string) scenario.Expectation {
	return &expectOutputAction{contains: contains}
}

func (e *expectOutputAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	// Check session output if available
	sess := environ.sessions[clientName]
	if sess != nil {
		output := sess.Output()
		if strings.Contains(output, e.contains) {
			return nil
		}
		return fmt.Errorf("output %q does not contain %q", truncate(output, 200), e.contains)
	}

	// Check last exec result
	if environ.lastExecResult != nil {
		combined := environ.lastExecResult.Stdout + environ.lastExecResult.Stderr
		if strings.Contains(combined, e.contains) {
			return nil
		}
		return fmt.Errorf("output %q does not contain %q", truncate(combined, 200), e.contains)
	}

	return fmt.Errorf("no output available to check")
}

func (e *expectOutputAction) String() string {
	return fmt.Sprintf("ExpectOutput(%q)", e.contains)
}

// expectOutputMatchAction checks that output matches a regex.
type expectOutputMatchAction struct {
	pattern string
	regex   *regexp.Regexp
}

// ExpectOutputMatch returns an expectation that output matches a regex.
func ExpectOutputMatch(pattern string) scenario.Expectation {
	regex, _ := regexp.Compile(pattern)
	return &expectOutputMatchAction{pattern: pattern, regex: regex}
}

func (e *expectOutputMatchAction) Check(ctx context.Context, env interface{}, clientName string) error {
	if e.regex == nil {
		return fmt.Errorf("invalid regex pattern: %s", e.pattern)
	}

	environ := env.(*Environment)
	// Check session output if available
	sess := environ.sessions[clientName]
	if sess != nil {
		output := sess.Output()
		if e.regex.MatchString(output) {
			return nil
		}
		return fmt.Errorf("output %q does not match pattern %q", truncate(output, 200), e.pattern)
	}

	// Check last exec result
	if environ.lastExecResult != nil {
		combined := environ.lastExecResult.Stdout + environ.lastExecResult.Stderr
		if e.regex.MatchString(combined) {
			return nil
		}
		return fmt.Errorf("output %q does not match pattern %q", truncate(combined, 200), e.pattern)
	}

	return fmt.Errorf("no output available to check")
}

func (e *expectOutputMatchAction) String() string {
	return fmt.Sprintf("ExpectOutputMatch(%q)", e.pattern)
}

// expectExitCodeAction checks the exit code.
type expectExitCodeAction struct {
	code int
}

// ExpectExitCode returns an expectation that the exit code matches.
func ExpectExitCode(code int) scenario.Expectation {
	return &expectExitCodeAction{code: code}
}

func (e *expectExitCodeAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	if environ.lastExecResult == nil {
		return fmt.Errorf("no exec result available")
	}
	if environ.lastExecResult.ExitCode != e.code {
		return fmt.Errorf("expected exit code %d, got %d", e.code, environ.lastExecResult.ExitCode)
	}
	return nil
}

func (e *expectExitCodeAction) String() string {
	return fmt.Sprintf("ExpectExitCode(%d)", e.code)
}

// expectEventAction checks that an event occurred.
type expectEventAction struct {
	eventID string
	attrs   []string
}

// ExpectEvent returns an expectation that an event occurred.
func ExpectEvent(eventID string, attrs ...string) scenario.Expectation {
	return &expectEventAction{eventID: eventID, attrs: attrs}
}

func (e *expectEventAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	if environ.events.Has(e.eventID, e.attrs...) {
		return nil
	}
	return fmt.Errorf("event %q with attrs %v not found", e.eventID, e.attrs)
}

func (e *expectEventAction) String() string {
	if len(e.attrs) > 0 {
		return fmt.Sprintf("ExpectEvent(%q, %v)", e.eventID, e.attrs)
	}
	return fmt.Sprintf("ExpectEvent(%q)", e.eventID)
}

// expectNoEventAction checks that an event did NOT occur.
type expectNoEventAction struct {
	eventID string
	attrs   []string
}

// ExpectNoEvent returns an expectation that an event did NOT occur.
func ExpectNoEvent(eventID string, attrs ...string) scenario.Expectation {
	return &expectNoEventAction{eventID: eventID, attrs: attrs}
}

func (e *expectNoEventAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	if !environ.events.Has(e.eventID, e.attrs...) {
		return nil
	}
	return fmt.Errorf("unexpected event %q with attrs %v found", e.eventID, e.attrs)
}

func (e *expectNoEventAction) String() string {
	if len(e.attrs) > 0 {
		return fmt.Sprintf("ExpectNoEvent(%q, %v)", e.eventID, e.attrs)
	}
	return fmt.Sprintf("ExpectNoEvent(%q)", e.eventID)
}

// expectConnectedAction checks that the client is connected.
type expectConnectedAction struct{}

// ExpectConnected returns an expectation that the client is connected.
func ExpectConnected() scenario.Expectation {
	return &expectConnectedAction{}
}

func (e *expectConnectedAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	client := environ.clients[clientName]
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	if !client.IsConnected() {
		return fmt.Errorf("client %q is not connected", clientName)
	}
	return nil
}

func (e *expectConnectedAction) String() string {
	return "ExpectConnected"
}

// expectDisconnectedAction checks that the client is disconnected.
type expectDisconnectedAction struct{}

// ExpectDisconnected returns an expectation that the client is disconnected.
func ExpectDisconnected() scenario.Expectation {
	return &expectDisconnectedAction{}
}

func (e *expectDisconnectedAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	client := environ.clients[clientName]
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	if client.IsConnected() {
		return fmt.Errorf("client %q is still connected", clientName)
	}
	return nil
}

func (e *expectDisconnectedAction) String() string {
	return "ExpectDisconnected"
}

// expectStdoutAction checks stdout specifically.
type expectStdoutAction struct {
	contains string
}

// ExpectStdout returns an expectation that stdout contains text.
func ExpectStdout(contains string) scenario.Expectation {
	return &expectStdoutAction{contains: contains}
}

func (e *expectStdoutAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	if environ.lastExecResult == nil {
		return fmt.Errorf("no exec result available")
	}
	if strings.Contains(environ.lastExecResult.Stdout, e.contains) {
		return nil
	}
	return fmt.Errorf("stdout %q does not contain %q", truncate(environ.lastExecResult.Stdout, 200), e.contains)
}

func (e *expectStdoutAction) String() string {
	return fmt.Sprintf("ExpectStdout(%q)", e.contains)
}

// expectStderrAction checks stderr specifically.
type expectStderrAction struct {
	contains string
}

// ExpectStderr returns an expectation that stderr contains text.
func ExpectStderr(contains string) scenario.Expectation {
	return &expectStderrAction{contains: contains}
}

func (e *expectStderrAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	if environ.lastExecResult == nil {
		return fmt.Errorf("no exec result available")
	}
	if strings.Contains(environ.lastExecResult.Stderr, e.contains) {
		return nil
	}
	return fmt.Errorf("stderr %q does not contain %q", truncate(environ.lastExecResult.Stderr, 200), e.contains)
}

func (e *expectStderrAction) String() string {
	return fmt.Sprintf("ExpectStderr(%q)", e.contains)
}

// expectScreenAction checks the PTY screen capture.
type expectScreenAction struct {
	contains string
}

// ExpectScreen returns an expectation that the screen contains text.
func ExpectScreen(contains string) scenario.Expectation {
	return &expectScreenAction{contains: contains}
}

func (e *expectScreenAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	sess := environ.sessions[clientName]
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	output := sess.Output()
	if strings.Contains(output, e.contains) {
		return nil
	}
	return fmt.Errorf("screen %q does not contain %q", truncate(output, 200), e.contains)
}

func (e *expectScreenAction) String() string {
	return fmt.Sprintf("ExpectScreen(%q)", e.contains)
}

// expectWaitForOutputAction waits for output to contain text.
type expectWaitForOutputAction struct {
	contains string
	timeout  time.Duration
}

// ExpectWaitForOutput returns an expectation that waits for output to contain text.
func ExpectWaitForOutput(contains string, timeout time.Duration) scenario.Expectation {
	return &expectWaitForOutputAction{contains: contains, timeout: timeout}
}

func (e *expectWaitForOutputAction) Check(ctx context.Context, env interface{}, clientName string) error {
	environ := env.(*Environment)
	sess := environ.sessions[clientName]
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	return sess.WaitForOutput(e.contains, e.timeout)
}

func (e *expectWaitForOutputAction) String() string {
	return fmt.Sprintf("ExpectWaitForOutput(%q, %s)", e.contains, e.timeout)
}

// customExpectation allows arbitrary functions as expectations.
type customExpectation struct {
	name string
	fn   func(ctx context.Context, env *Environment, clientName string) error
}

// CustomExpectation returns an expectation that executes a custom function.
func CustomExpectation(name string, fn func(ctx context.Context, env *Environment, clientName string) error) scenario.Expectation {
	return &customExpectation{name: name, fn: fn}
}

func (e *customExpectation) Check(ctx context.Context, env interface{}, clientName string) error {
	return e.fn(ctx, env.(*Environment), clientName)
}

func (e *customExpectation) String() string {
	return e.name
}

// truncate truncates a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
