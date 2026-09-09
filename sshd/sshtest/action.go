package sshtest

import (
	"context"
	"fmt"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

// connectAction connects the client.
type connectAction struct{}

// Connect returns an action that connects the client.
func Connect() Action {
	return actionAdapter{&connectAction{}}
}

func (a *connectAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	return client.Connect()
}

func (a *connectAction) String() string {
	return "Connect"
}

// disconnectAction disconnects the client.
type disconnectAction struct{}

// Disconnect returns an action that disconnects the client.
func Disconnect() Action {
	return actionAdapter{&disconnectAction{}}
}

func (a *disconnectAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	return client.Close()
}

func (a *disconnectAction) String() string {
	return "Disconnect"
}

// execAction executes a command.
type execAction struct {
	cmd    string
	result **ExecResult // Pointer to store result
}

// Exec returns an action that executes a command.
func Exec(cmd string) Action {
	return actionAdapter{&execAction{cmd: cmd}}
}

// ExecWithResult returns an action that executes a command and stores the result.
func ExecWithResult(cmd string, result **ExecResult) Action {
	return actionAdapter{&execAction{cmd: cmd, result: result}}
}

func (a *execAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	result, err := client.Exec(a.cmd)
	if err != nil {
		return err
	}
	if a.result != nil {
		*a.result = result
	}
	// Store result in context for expectations
	if !e.storeExecResult(result) {
		return fmt.Errorf("environment stopped while publishing exec result for client %q", clientName)
	}
	return nil
}

func (a *execAction) String() string {
	return fmt.Sprintf("Exec(%q)", a.cmd)
}

// shellAction starts a shell session.
type shellAction struct {
	session *Session // Pointer to store session
}

// StartShell returns an action that starts a shell session.
func StartShell() Action {
	return actionAdapter{&shellAction{}}
}

// StartShellWithSession returns an action that starts a shell and stores the session.
func StartShellWithSession(session *Session) Action {
	return actionAdapter{&shellAction{session: session}}
}

func (a *shellAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	sess, err := client.Shell()
	if err != nil {
		return err
	}
	if a.session != nil {
		*a.session = sess
	}
	// Store session in environment for other actions
	if !e.storeSession(clientName, sess) {
		_ = sess.Close()
		return fmt.Errorf("environment stopped while starting shell for client %q", clientName)
	}
	return nil
}

func (a *shellAction) String() string {
	return "StartShell"
}

// closeShellAction closes the shell session.
type closeShellAction struct{}

// CloseShell returns an action that closes the shell session.
func CloseShell() Action {
	return actionAdapter{&closeShellAction{}}
}

func (a *closeShellAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	sess := e.takeSession(clientName)
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	return sess.Close()
}

func (a *closeShellAction) String() string {
	return "CloseShell"
}

// sendInputAction sends input to the shell.
type sendInputAction struct {
	text string
}

// SendInput returns an action that sends input to the shell.
func SendInput(text string) Action {
	return actionAdapter{&sendInputAction{text: text}}
}

func (a *sendInputAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	sess := e.sessionByName(clientName)
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	_, err := sess.Write([]byte(a.text))
	return err
}

func (a *sendInputAction) String() string {
	return fmt.Sprintf("SendInput(%q)", a.text)
}

// sendKeyAction sends a special key to the shell.
type sendKeyAction struct {
	key scenario.Key
}

// SendKey returns an action that sends a special key to the shell.
func SendKey(key scenario.Key) Action {
	return actionAdapter{&sendKeyAction{key: key}}
}

func (a *sendKeyAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	sess := e.sessionByName(clientName)
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	_, err := sess.Write([]byte(a.key))
	return err
}

func (a *sendKeyAction) String() string {
	return fmt.Sprintf("SendKey(%s)", scenario.KeyLabel(a.key))
}

// sendLineAction sends text followed by Enter.
type sendLineAction struct {
	text string
}

// SendLine returns an action that sends text followed by Enter.
func SendLine(text string) Action {
	return actionAdapter{&sendLineAction{text: text}}
}

func (a *sendLineAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	sess := e.sessionByName(clientName)
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	_, err := sess.Write([]byte(a.text + "\r"))
	return err
}

func (a *sendLineAction) String() string {
	return fmt.Sprintf("SendLine(%q)", a.text)
}

// resizePTYAction resizes the PTY.
type resizePTYAction struct {
	cols uint32
	rows uint32
}

// ResizePTY returns an action that resizes the PTY.
func ResizePTY(cols, rows uint32) Action {
	return actionAdapter{&resizePTYAction{cols: cols, rows: rows}}
}

func (a *resizePTYAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	sess := e.sessionByName(clientName)
	if sess == nil {
		return fmt.Errorf("no active session for client %q", clientName)
	}
	return sess.Resize(a.cols, a.rows)
}

func (a *resizePTYAction) String() string {
	return fmt.Sprintf("ResizePTY(%d, %d)", a.cols, a.rows)
}

// sleepAction waits for a duration.
type sleepAction struct {
	duration time.Duration
}

// Sleep returns an action that waits for a duration.
func Sleep(d time.Duration) Action {
	return actionAdapter{&sleepAction{duration: d}}
}

func (a *sleepAction) execute(ctx context.Context, env *Environment, clientName string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(a.duration):
		return nil
	}
}

func (a *sleepAction) String() string {
	return fmt.Sprintf("Sleep(%s)", a.duration)
}

// waitForEventAction waits for an event.
type waitForEventAction struct {
	eventID string
	attrs   []string
	timeout time.Duration
}

// WaitForEvent returns an action that waits for an event.
func WaitForEvent(eventID string, attrs ...string) Action {
	return actionAdapter{&waitForEventAction{
		eventID: eventID,
		attrs:   attrs,
		timeout: 10 * time.Second,
	}}
}

// WaitForEventTimeout returns an action that waits for an event with a custom timeout.
func WaitForEventTimeout(timeout time.Duration, eventID string, attrs ...string) Action {
	return actionAdapter{&waitForEventAction{
		eventID: eventID,
		attrs:   attrs,
		timeout: timeout,
	}}
}

func (a *waitForEventAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	_, err := e.events.WaitTimeout(a.timeout, a.eventID, a.attrs...)
	return err
}

func (a *waitForEventAction) String() string {
	if len(a.attrs) > 0 {
		return fmt.Sprintf("WaitForEvent(%q, %v)", a.eventID, a.attrs)
	}
	return fmt.Sprintf("WaitForEvent(%q)", a.eventID)
}

// customAction allows arbitrary functions as actions.
type customAction struct {
	name string
	fn   func(ctx context.Context, env *Environment, clientName string) error
}

// Custom returns an action that executes a custom function.
func Custom(name string, fn func(ctx context.Context, env *Environment, clientName string) error) Action {
	return actionAdapter{&customAction{name: name, fn: fn}}
}

func (a *customAction) execute(ctx context.Context, env *Environment, clientName string) error {
	return a.fn(ctx, env, clientName)
}

func (a *customAction) String() string {
	return a.name
}
