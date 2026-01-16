package xssh

import (
	"encoding/binary"
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
	// Resolve shell path if not already done
	if c.config.Shell == "" || !isAbsPath(c.config.Shell) {
		if path, err := ShellPath(c.config.Shell); err == nil {
			c.config.Shell = path
		}
	}
	c.sessionRequestHandlers["pty-req"] = handlePtyReq
	c.sessionRequestHandlers["window-change"] = handleWindowChange
	c.sessionRequestHandlers["env"] = handleEnv
	c.sessionRequestHandlers["shell"] = handleShell
	c.sessionRequestHandlers["exec"] = handleExec
}

// isAbsPath checks if the path is absolute (simple check for leading /)
func isAbsPath(path string) bool {
	return len(path) > 0 && (path[0] == '/' || (len(path) > 1 && path[1] == ':'))
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
	if len(req.Payload) < 4 {
		return fmt.Errorf("malformed pty-req payload")
	}
	termLen := req.Payload[3]
	sess.Resizes <- req.Payload[termLen+4:]
	debugf(sess, "PTY ready")
	return nil
}

// handleWindowChange handles "window-change" session requests
func handleWindowChange(sess *Session, req *Request) error {
	sess.Resizes <- req.Payload
	return nil
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

	// Execute the command
	go executeCommand(sess, command)
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

	// start a shell for this channel's connection
	shellf, err := startPTY(shell)
	if err != nil {
		closeFunc()
		return fmt.Errorf("could not start pty: %w", err)
	}

	// dequeue resizes
	go func() {
		for payload := range sess.Resizes {
			w, h := parseDims(payload)
			SetWinsize(shellf, w, h)
		}
	}()

	// pipe session to shell and visa-versa
	var once sync.Once
	go func() {
		_, err := io.Copy(sess.Channel, shellf)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Shell to connection copy error: %s", err)
		}
		once.Do(closeFunc)
	}()
	go func() {
		_, err := io.Copy(shellf, sess.Channel)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Connection to shell copy error: %s", err)
		}
		once.Do(closeFunc)
	}()

	debugf(sess, "Shell attached")

	go func() {
		// Start proactively listening for process death, for those ptys that
		// don't signal on EOF.
		if shell.Process != nil {
			_, err := shell.Process.Wait()
			if err != nil {
				if !strings.Contains(err.Error(), "wait: no child processes") && !strings.Contains(err.Error(), "exit status") && !strings.Contains(err.Error(), "Wait was already called") {
					errorf(sess, "Shell process wait error: %s", err)
				}
			}
			// Closing the pty is idempotent and ensures the copy goroutines exit
			shellf.Close()
		}
		debugf(sess, "Shell terminated")
		once.Do(closeFunc)
	}()

	return nil
}

// executeCommand executes a shell command and pipes the output to the SSH connection
func executeCommand(sess *Session, command string) {
	defer sess.Channel.Close()
	cfg := sess.Config()

	// Use shell to execute the command
	cmd := exec.Command(cfg.Shell, "-c", command)
	setSysProcAttr(cmd)
	if cfg.WorkingDirectory != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Env = sess.Env
	cmd.Stdin = sess.Channel
	cmd.Stdout = sess.Channel
	cmd.Stderr = sess.Channel

	// capture exit status
	type exit struct {
		Status uint32
	}

	// Run the command
	err := cmd.Run()
	exitCode := uint32(0)
	if err != nil {
		debugf(sess, "Command execution failed: %s", err)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = uint32(exitErr.ExitCode())
		}
	}
	debugf(sess, "Command execution completed")
	if _, err := sess.Channel.SendRequest("exit-status", false, ssh.Marshal(&exit{Status: exitCode})); err != nil {
		debugf(sess, "Failed to send exit-status: %s", err)
	}
}
