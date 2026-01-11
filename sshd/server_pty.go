package sshd

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

// handlePtyReq handles "pty-req" session requests
func handlePtyReq(sess *Session, req *Request) error {
	if len(req.Payload) < 4 {
		return fmt.Errorf("malformed pty-req payload")
	}
	termLen := req.Payload[3]
	sess.Resizes <- req.Payload[termLen+4:]
	sess.Debugf("PTY ready")
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
	sess.Debugf("env: %s", kv)
	if !sess.Config().IgnoreEnv {
		sess.Env = appendEnv(sess.Env, kv)
	}
	return nil
}

// handleShell handles "shell" session requests
func handleShell(sess *Session, req *Request) error {
	if len(req.Payload) > 0 {
		sess.Debugf("shell command ignored '%s'", req.Payload)
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
	sess.Debugf("exec command: %s", command)

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
	if cfg.WorkDir != "" {
		shell.Dir = cfg.WorkDir
	}
	if !hasEnv(sess.Env, "TERM") {
		sess.Env = append(sess.Env, "TERM=xterm-256color")
	}
	shell.Env = sess.Env
	sess.Debugf("Session env: %v", sess.Env)

	closeFunc := func() {
		sess.Channel.Close()
		if shell.Process != nil {
			signalErr := shell.Process.Signal(os.Interrupt)
			if signalErr != nil && !strings.Contains(signalErr.Error(), "process already finished") && !strings.Contains(signalErr.Error(), "already exited") && !strings.Contains(signalErr.Error(), "not supported") {
				sess.Errorf("Failed to interrupt shell: %s", signalErr)
			}
			time.Sleep(100 * time.Millisecond)
			killErr := shell.Process.Kill()
			if killErr != nil && !strings.Contains(killErr.Error(), "process already finished") && !strings.Contains(killErr.Error(), "already exited") && !strings.Contains(killErr.Error(), "not supported") {
				sess.Errorf("Failed to kill shell: %s", killErr)
			}
			if _, waitErr := shell.Process.Wait(); waitErr != nil {
				sess.Debugf("Process wait error: %s", waitErr)
			}
		}
		sess.Debugf("Session closed")
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
			sess.Debugf("Shell to connection copy error: %s", err)
		}
		once.Do(closeFunc)
	}()
	go func() {
		_, err := io.Copy(shellf, sess.Channel)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			sess.Debugf("Connection to shell copy error: %s", err)
		}
		once.Do(closeFunc)
	}()

	sess.Debugf("Shell attached")

	go func() {
		// Start proactively listening for process death, for those ptys that
		// don't signal on EOF.
		if shell.Process != nil {
			_, err := shell.Process.Wait()
			if err != nil {
				if !strings.Contains(err.Error(), "wait: no child processes") && !strings.Contains(err.Error(), "exit status") && !strings.Contains(err.Error(), "Wait was already called") {
					sess.Errorf("Shell process wait error: %s", err)
				}
			}
			// Closing the pty is idempotent and ensures the copy goroutines exit
			shellf.Close()
		}
		sess.Debugf("Shell terminated")
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
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
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
		sess.Debugf("Command execution failed: %s", err)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = uint32(exitErr.ExitCode())
		}
	}
	sess.Debugf("Command execution completed")
	if _, err := sess.Channel.SendRequest("exit-status", false, ssh.Marshal(&exit{Status: exitCode})); err != nil {
		sess.Debugf("Failed to send exit-status: %s", err)
	}
}
