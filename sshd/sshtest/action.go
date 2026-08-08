package sshtest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

// connectAction connects the client.
type connectAction struct{}

// Connect returns an action that connects the client.
func Connect() scenario.Action {
	return &connectAction{}
}

func (a *connectAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func Disconnect() scenario.Action {
	return &disconnectAction{}
}

func (a *disconnectAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func Exec(cmd string) scenario.Action {
	return &execAction{cmd: cmd}
}

// ExecWithResult returns an action that executes a command and stores the result.
func ExecWithResult(cmd string, result **ExecResult) scenario.Action {
	return &execAction{cmd: cmd, result: result}
}

func (a *execAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func StartShell() scenario.Action {
	return &shellAction{}
}

// StartShellWithSession returns an action that starts a shell and stores the session.
func StartShellWithSession(session *Session) scenario.Action {
	return &shellAction{session: session}
}

func (a *shellAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func CloseShell() scenario.Action {
	return &closeShellAction{}
}

func (a *closeShellAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func SendInput(text string) scenario.Action {
	return &sendInputAction{text: text}
}

func (a *sendInputAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func SendKey(key scenario.Key) scenario.Action {
	return &sendKeyAction{key: key}
}

func (a *sendKeyAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func SendLine(text string) scenario.Action {
	return &sendLineAction{text: text}
}

func (a *sendLineAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func ResizePTY(cols, rows uint32) scenario.Action {
	return &resizePTYAction{cols: cols, rows: rows}
}

func (a *resizePTYAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
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
func Sleep(d time.Duration) scenario.Action {
	return &sleepAction{duration: d}
}

func (a *sleepAction) Execute(ctx context.Context, env interface{}, clientName string) error {
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
func WaitForEvent(eventID string, attrs ...string) scenario.Action {
	return &waitForEventAction{
		eventID: eventID,
		attrs:   attrs,
		timeout: 10 * time.Second,
	}
}

// WaitForEventTimeout returns an action that waits for an event with a custom timeout.
func WaitForEventTimeout(timeout time.Duration, eventID string, attrs ...string) scenario.Action {
	return &waitForEventAction{
		eventID: eventID,
		attrs:   attrs,
		timeout: timeout,
	}
}

func (a *waitForEventAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
	_, err := e.events.WaitTimeout(a.timeout, a.eventID, a.attrs...)
	return err
}

func (a *waitForEventAction) String() string {
	if len(a.attrs) > 0 {
		return fmt.Sprintf("WaitForEvent(%q, %v)", a.eventID, a.attrs)
	}
	return fmt.Sprintf("WaitForEvent(%q)", a.eventID)
}

// sftpUploadAction uploads a file via SFTP.
type sftpUploadAction struct {
	localPath  string
	remotePath string
}

// SFTPUpload returns an action that uploads a file via SFTP.
func SFTPUpload(localPath, remotePath string) scenario.Action {
	return &sftpUploadAction{localPath: localPath, remotePath: remotePath}
}

func (a *sftpUploadAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	return executeSFTPOperation(ctx, env, clientName, "upload", func(client *SFTPClient) error {
		if err := client.Upload(a.localPath, a.remotePath); err != nil {
			return fmt.Errorf("upload local path %q to remote path %q: %w", a.localPath, a.remotePath, err)
		}
		return nil
	})
}

func (a *sftpUploadAction) String() string {
	return fmt.Sprintf("SFTPUpload(%q, %q)", a.localPath, a.remotePath)
}

// sftpDownloadAction downloads a file via SFTP.
type sftpDownloadAction struct {
	remotePath string
	localPath  string
}

// SFTPDownload returns an action that downloads a file via SFTP.
func SFTPDownload(remotePath, localPath string) scenario.Action {
	return &sftpDownloadAction{remotePath: remotePath, localPath: localPath}
}

func (a *sftpDownloadAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	return executeSFTPOperation(ctx, env, clientName, "download", func(client *SFTPClient) error {
		if err := client.Download(a.remotePath, a.localPath); err != nil {
			return fmt.Errorf("download remote path %q to local path %q: %w", a.remotePath, a.localPath, err)
		}
		return nil
	})
}

func (a *sftpDownloadAction) String() string {
	return fmt.Sprintf("SFTPDownload(%q, %q)", a.remotePath, a.localPath)
}

func executeSFTPOperation(ctx context.Context, rawEnv interface{}, clientName, operation string, fn func(*SFTPClient) error) error {
	e, ok := rawEnv.(*Environment)
	if !ok || e == nil {
		return fmt.Errorf("SFTP %s: invalid test environment %T", operation, rawEnv)
	}
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("SFTP %s canceled before session start: %w", operation, err)
	}

	sftpClient, err := client.SFTP()
	if err != nil {
		return fmt.Errorf("start SFTP session for %s: %w", operation, err)
	}
	done := make(chan error, 1)
	go func() { done <- fn(sftpClient) }()

	select {
	case err := <-done:
		if closeErr := sftpClient.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SFTP session after %s: %w", operation, closeErr))
		}
		return err
	case <-ctx.Done():
	}

	// pkg/sftp Close interrupts outstanding requests. Close synchronously, then
	// join the one operation goroutine: the action never abandons goroutines it
	// owns. Real controlled-transfer tests enforce the practical time bound.
	closeErr := sftpClient.Close()
	operationErr := <-done
	// The operation may have committed its result immediately before Close won
	// the lifecycle lock. In that case completion, not cancellation, is the
	// observable outcome even if this select happened to receive ctx.Done.
	if operationErr == nil {
		if closeErr != nil {
			return fmt.Errorf("close completed SFTP %s session: %w", operation, closeErr)
		}
		return nil
	}
	if operationErr != nil {
		operationErr = fmt.Errorf("SFTP %s stopped after cancellation: %w", operation, operationErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close canceled SFTP %s session: %w", operation, closeErr)
	}
	return errors.Join(fmt.Errorf("SFTP %s canceled: %w", operation, ctx.Err()), operationErr, closeErr)
}

// localForwardAction creates a local port forward.
type localForwardAction struct {
	localAddr  string
	remoteAddr string
}

// LocalForward returns an action that creates a local port forward.
func LocalForward(localAddr, remoteAddr string) scenario.Action {
	return &localForwardAction{localAddr: localAddr, remoteAddr: remoteAddr}
}

func (a *localForwardAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	listener, err := client.LocalForward(a.localAddr, a.remoteAddr)
	if err != nil {
		return err
	}
	// Store listener for cleanup
	if !e.storeForwardListener(clientName, listener) {
		_ = listener.Close()
		return fmt.Errorf("environment stopped while creating local forward for client %q", clientName)
	}
	return nil
}

func (a *localForwardAction) String() string {
	return fmt.Sprintf("LocalForward(%q, %q)", a.localAddr, a.remoteAddr)
}

// remoteForwardAction creates a remote port forward.
type remoteForwardAction struct {
	remoteAddr string
	localAddr  string
}

// RemoteForward returns an action that creates a remote port forward.
func RemoteForward(remoteAddr, localAddr string) scenario.Action {
	return &remoteForwardAction{remoteAddr: remoteAddr, localAddr: localAddr}
}

func (a *remoteForwardAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	e := env.(*Environment)
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	return client.RemoteForward(a.remoteAddr, a.localAddr)
}

func (a *remoteForwardAction) String() string {
	return fmt.Sprintf("RemoteForward(%q, %q)", a.remoteAddr, a.localAddr)
}

// customAction allows arbitrary functions as actions.
type customAction struct {
	name string
	fn   func(ctx context.Context, env *Environment, clientName string) error
}

// Custom returns an action that executes a custom function.
func Custom(name string, fn func(ctx context.Context, env *Environment, clientName string) error) scenario.Action {
	return &customAction{name: name, fn: fn}
}

func (a *customAction) Execute(ctx context.Context, env interface{}, clientName string) error {
	return a.fn(ctx, env.(*Environment), clientName)
}

func (a *customAction) String() string {
	return a.name
}
