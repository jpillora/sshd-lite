package xssh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// registerSessionHandlers registers the built-in session request handlers
// when Session is enabled.
func (c *xconn) registerSessionHandlers() {
	if !c.config.Session {
		return
	}
	// Resolve shell path if the caller didn't. Safe to write: c.config is this
	// connection's own copy, not the one shared across connections.
	if !filepath.IsAbs(c.config.Shell) {
		if path, err := ShellPath(c.config.Shell); err == nil {
			c.config.Shell = path
		}
	}
	c.sessionRequestHandlers[PTYRequestType] = handlePtyReq
	c.sessionRequestHandlers[WindowChangeRequestType] = handleWindowChange
	c.sessionRequestHandlers[EnvRequestType] = handleEnv
	c.sessionRequestHandlers[ShellRequestType] = handleShell
	c.sessionRequestHandlers[ExecRequestType] = handleExec
}

// session logging helpers
func debugf(sess *Session, f string, args ...interface{}) {
	if sess.Logger != nil {
		sess.Logger.Debug(fmt.Sprintf(f, args...))
	}
}

func errorf(sess *Session, f string, args ...interface{}) {
	if sess.Logger != nil {
		sess.Logger.Error(fmt.Sprintf(f, args...))
	}
}

// handlePtyReq handles "pty-req" session requests
func handlePtyReq(sess *Session, req *Request) error {
	ws, err := parsePtyRequest(req.Payload)
	if err != nil {
		return err
	}
	queueResize(sess, ws)
	debugf(sess, "PTY ready")
	return nil
}

// handleWindowChange handles "window-change" session requests
func handleWindowChange(sess *Session, req *Request) error {
	ws, err := parseWindowChange(req.Payload)
	if err != nil {
		return err
	}
	queueResize(sess, ws)
	return nil
}

type ptyRequestPayload struct {
	Term                    string
	Columns, Rows           uint32
	PixelWidth, PixelHeight uint32
	Modes                   string
}

type windowChangePayload struct {
	Columns, Rows           uint32
	PixelWidth, PixelHeight uint32
}

func parsePtyRequest(payload []byte) (*Winsize, error) {
	var p ptyRequestPayload
	if err := ssh.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("malformed pty-req payload: %w", err)
	}
	if err := validateTerminalModes([]byte(p.Modes)); err != nil {
		return nil, fmt.Errorf("malformed pty-req terminal modes: %w", err)
	}
	ws, err := winsizeFromDimensions(p.Columns, p.Rows)
	if err != nil {
		return nil, fmt.Errorf("invalid pty-req dimensions: %w", err)
	}
	return ws, nil
}

func parseWindowChange(payload []byte) (*Winsize, error) {
	var p windowChangePayload
	if err := ssh.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("malformed window-change payload: %w", err)
	}
	ws, err := winsizeFromDimensions(p.Columns, p.Rows)
	if err != nil {
		return nil, fmt.Errorf("invalid window-change dimensions: %w", err)
	}
	return ws, nil
}

// validateTerminalModes checks the framing from RFC 4254 section 8. Opcodes
// 1-159 carry one uint32 argument. Opcode 0 terminates the stream. Opcodes
// 160-255 are undefined and cause parsing to stop, including when bytes remain
// in the modes string because their extension framing is unknown to us.
//
// An exhausted stream also ends parsing. TTY_OP_END is not required, because
// clients built on libssh2 send an empty modes string when the caller supplies
// no modes, and OpenSSH's own parser stops at the end of the buffer too.
// Rejecting those would fail pty-req for interoperable clients.
func validateTerminalModes(modes []byte) error {
	for len(modes) > 0 {
		opcode := modes[0]
		modes = modes[1:]
		switch {
		case opcode == 0:
			if len(modes) != 0 {
				return fmt.Errorf("%d trailing bytes after TTY_OP_END", len(modes))
			}
			return nil
		case opcode <= 159:
			if len(modes) < 4 {
				return fmt.Errorf("opcode %d has an incomplete uint32 argument", opcode)
			}
			modes = modes[4:]
		default:
			return nil
		}
	}
	return nil
}

// queueResize gives resize traffic latest-value semantics. A slow PTY resize
// consumer cannot stall request handling, and the next applied size is always
// the newest request observed by the dispatcher. A nil size represents RFC
// 4254's required handling for zero dimensions and is intentionally ignored.
func queueResize(sess *Session, ws *Winsize) {
	if ws == nil {
		return
	}
	payload := marshalDims(ws)
	select {
	case sess.Resizes <- payload:
		return
	default:
	}
	for {
		select {
		case <-sess.Resizes:
			continue
		default:
		}
		break
	}
	select {
	case sess.Resizes <- payload:
	default:
		// A direct external producer may have won the slot. Requests handled
		// by the built-in dispatcher are serialized and do not take this path.
	}
}

// handleEnv handles "env" session requests
func handleEnv(sess *Session, req *Request) error {
	e := struct{ Name, Value string }{}
	if err := ssh.Unmarshal(req.Payload, &e); err != nil {
		return fmt.Errorf("failed to unmarshal env: %w", err)
	}
	kv := e.Name + "=" + e.Value
	debugf(sess, "env: %s", kv)
	if !sess.Config().IgnoreEnv {
		sess.Env = appendEnv(sess.Env, kv)
	}
	return nil
}

// handleShell handles "shell" session requests
func handleShell(sess *Session, req *Request) error {
	if len(req.Payload) > 0 {
		debugf(sess, "shell command ignored '%s'", req.Payload)
	}
	return attachShell(sess)
}

// handleExec handles "exec" session requests
func handleExec(sess *Session, req *Request) error {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// command name is a string encoded as: [uint32 length][string command]
	if len(req.Payload) < 4 {
		return fmt.Errorf("malformed exec request payload")
	}
	length := binary.BigEndian.Uint32(req.Payload)
	if uint32(len(req.Payload)-4) != length {
		return fmt.Errorf("command length mismatch in payload")
	}
	command := string(req.Payload[4:])
	debugf(sess, "exec command: %s", command)

	// A command can fail and close its channel immediately. Acknowledge the
	// request before starting it so exit-status and channel-close packets cannot
	// overtake the request reply and surface as EOF from ssh.Session.Start.
	if req.WantReply {
		if err := req.Reply(true, nil); err != nil {
			return fmt.Errorf("failed to accept exec request: %w", err)
		}
	}

	// Execute the command.
	sess.goTask(func() { executeCommand(sess, command) })
	return nil
}

// attachShell attaches a shell to the session
func attachShell(sess *Session) error {
	cfg := sess.Config()
	args := []string{}
	switch filepath.Base(cfg.Shell) {
	case "bash", "fish":
		args = append(args, "-l")
	}
	shell := exec.Command(cfg.Shell, args...)
	setSysProcAttr(shell)
	if cfg.WorkingDirectory != "" {
		shell.Dir = cfg.WorkingDirectory
	}
	if !hasEnv(sess.Env, "TERM") {
		sess.Env = append(sess.Env, "TERM=xterm-256color")
	}
	shell.Env = sess.Env
	debugf(sess, "Session env: %v", sess.Env)

	closeFunc := func() {
		sess.Channel.Close()
		if shell.Process != nil {
			signalErr := shell.Process.Signal(os.Interrupt)
			if signalErr != nil && !strings.Contains(signalErr.Error(), "process already finished") && !strings.Contains(signalErr.Error(), "already exited") && !strings.Contains(signalErr.Error(), "not supported") {
				errorf(sess, "Failed to interrupt shell: %s", signalErr)
			}
			time.Sleep(100 * time.Millisecond)
			killErr := shell.Process.Kill()
			if killErr != nil && !strings.Contains(killErr.Error(), "process already finished") && !strings.Contains(killErr.Error(), "already exited") && !strings.Contains(killErr.Error(), "not supported") {
				errorf(sess, "Failed to kill shell: %s", killErr)
			}
			if _, waitErr := shell.Process.Wait(); waitErr != nil {
				debugf(sess, "Process wait error: %s", waitErr)
			}
		}
		debugf(sess, "Session closed")
	}

	// drain initial PTY size from pty-req handler (avoids race where
	// shell starts at OS default 80x24 before the resize goroutine fires)
	var initialSize *Winsize
	select {
	case payload := <-sess.Resizes:
		ws, err := parseDims(payload)
		if err != nil {
			errorf(sess, "Ignored invalid initial PTY size: %s", err)
		} else {
			initialSize = ws
		}
	default:
	}

	// start a shell for this channel's connection
	shellf, err := startPTY(shell, initialSize)
	if err != nil {
		closeFunc()
		return fmt.Errorf("could not start pty: %w", err)
	}

	// On platforms where this package owns PTY teardown (Unix), serialize it
	// with resize operations. Windows running-PTY resizes are disabled below
	// because its backend closes the ConPTY independently of this lock.
	var ptyMu sync.Mutex
	ptyClosed := false

	// dequeue resizes
	sess.goTask(func() {
		for payload := range sess.Resizes {
			ws, err := parseDims(payload)
			if err != nil {
				errorf(sess, "Ignored invalid PTY resize: %s", err)
				continue
			}
			if ws == nil {
				continue
			}
			if !supportsRunningPTYResize {
				// photostorm/pty closes the Windows ConPTY from its own process
				// waiter. It exposes no lifecycle lock or closed signal, so any
				// post-start ResizePseudoConsole call can race that close. The
				// initial size was already applied atomically by StartWithSize.
				continue
			}
			ptyMu.Lock()
			if ptyClosed {
				ptyMu.Unlock()
				continue
			}
			err = SetWinsize(shellf, uint32(ws.Cols), uint32(ws.Rows))
			ptyMu.Unlock()
			if err != nil {
				errorf(sess, "SetWinsize failed: %s", err)
			}
		}
	})

	// pipe session to shell and visa-versa
	var once sync.Once
	sess.goTask(func() {
		_, err := io.Copy(sess.Channel, shellf)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Shell to connection copy error: %s", err)
		}
		once.Do(closeFunc)
	})
	sess.goTask(func() {
		_, err := io.Copy(shellf, sess.Channel)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Connection to shell copy error: %s", err)
		}
		once.Do(closeFunc)
	})

	debugf(sess, "Shell attached")

	sess.goTask(func() {
		// Start proactively listening for process death, for those ptys that
		// don't signal on EOF.
		if shell.Process != nil {
			_, err := shell.Process.Wait()
			if err != nil {
				if !strings.Contains(err.Error(), "wait: no child processes") && !strings.Contains(err.Error(), "exit status") && !strings.Contains(err.Error(), "Wait was already called") {
					errorf(sess, "Shell process wait error: %s", err)
				}
			}
			// Release the pty so the io.Copy goroutines unblock. On Windows the
			// pty library closes the ConPTY itself; closing it again here would
			// double-free the pseudoconsole handle and corrupt the heap.
			ptyMu.Lock()
			if !ptyClosed {
				closeShellPTY(shellf)
				ptyClosed = true
			}
			ptyMu.Unlock()
		}
		debugf(sess, "Shell terminated")
		once.Do(closeFunc)
	})

	return nil
}

// executeCommand executes a shell command and pipes the output to the SSH connection
func executeCommand(sess *Session, command string) {
	defer sess.Channel.Close()
	cfg := sess.Config()

	// Use shell to execute the command
	cmd := exec.Command(cfg.Shell, "-c", command)
	prepareCommand(cmd)
	if cfg.WorkingDirectory != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Env = sess.Env
	cmd.Stdout = sess.Channel
	cmd.Stderr = sess.Channel.Stderr()
	// Use StdinPipe so cmd.Wait doesn't block waiting for sess.Channel to EOF.
	// Wait auto-closes the pipe when the process exits, unblocking our copy goroutine.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		commandSetupFailed(sess, "failed to create command stdin", err)
		return
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		action := "failed to start command"
		if cmd.Dir != "" {
			// Single quotes keep a Windows working directory readable; %q would
			// escape every path separator in the message sent to the client.
			action = fmt.Sprintf("failed to start command in '%s'", cmd.Dir)
		}
		commandSetupFailed(sess, action, err)
		return
	}

	sess.goTask(func() { copyCommandStdin(sess, stdin) })

	err, lifecycleErr := waitCommand(cmd, sess.Done())
	if lifecycleErr != nil && !processAlreadyDone(lifecycleErr) {
		debugf(sess, "Failed to observe or terminate command: %s", lifecycleErr)
	}

	exitCode := commandExitCode(err)
	if errors.Is(err, exec.ErrWaitDelay) {
		debugf(sess, "Command exited successfully but left its output pipes open; closed them after %s", commandWaitDelay)
	} else if err != nil {
		debugf(sess, "Command execution failed: %s", err)
	}
	debugf(sess, "Command execution completed")
	sendExitStatus(sess, exitCode)
}

const commandWaitDelay = 2 * time.Second

func prepareCommand(cmd *exec.Cmd) {
	setCommandProcessGroup(cmd)
	// os/exec uses internal pipes when stdout/stderr are non-files (including an
	// SSH channel). If descendants inherit those handles after the command
	// leader exits, WaitDelay closes the pipes instead of letting Wait hang.
	cmd.WaitDelay = commandWaitDelay
}

func copyCommandStdin(sess *Session, stdin io.WriteCloser) {
	_, err := io.Copy(stdin, sess.Channel)
	if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "broken pipe") {
		debugf(sess, "Connection to stdin copy error: %s", err)
	}
	// EOF on SSH stdin is deliberately not session cancellation. Closing only
	// the command's stdin preserves commands such as cat, wc, and sort.
	if err := stdin.Close(); err != nil && !processAlreadyDone(err) && !strings.Contains(err.Error(), "broken pipe") {
		debugf(sess, "Failed to close command stdin: %s", err)
	}
}

func commandSetupFailed(sess *Session, action string, err error) {
	message := fmt.Sprintf("sshd-lite: %s: %v\n", action, err)
	if _, writeErr := io.WriteString(sess.Channel.Stderr(), message); writeErr != nil {
		debugf(sess, "Failed to report command setup error: %s", writeErr)
	}
	debugf(sess, "%s: %s", action, err)
	sendExitStatus(sess, 1)
}

func commandExitCode(err error) uint32 {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return uint32(exitErr.ExitCode())
	}
	// Wait only substitutes ErrWaitDelay for a nil error, so the command itself
	// succeeded and only its inherited output pipes had to be force-closed.
	// Reporting a failure here would make backgrounded work look like an error.
	if errors.Is(err, exec.ErrWaitDelay) {
		return 0
	}
	return 1
}

func sendExitStatus(sess *Session, status uint32) {
	type exit struct {
		Status uint32
	}
	if _, err := sess.Channel.SendRequest("exit-status", false, ssh.Marshal(&exit{Status: status})); err != nil {
		debugf(sess, "Failed to send exit-status: %s", err)
	}
}

func processAlreadyDone(err error) bool {
	return err == nil ||
		errors.Is(err, os.ErrProcessDone) ||
		strings.Contains(err.Error(), "process already finished") ||
		strings.Contains(err.Error(), "already exited")
}
