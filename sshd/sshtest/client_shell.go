package sshtest

import (
	"fmt"
	"strings"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"golang.org/x/crypto/ssh"
)

// Shell starts an interactive shell session.
func (c *clientGo) Shell() (Session, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Set up PTY if configured
	cols, rows := uint32(80), uint32(24)
	if c.config.ptySize != nil {
		cols = c.config.ptySize.cols
		rows = c.config.ptySize.rows
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", int(rows), int(cols), modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to request PTY: %w", err)
	}

	c.events.Emit(scenario.EventPTYRequested, "client", c.config.name, "cols", fmt.Sprint(cols), "rows", fmt.Sprint(rows))

	// Get stdin/stdout pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	c.events.Emit(scenario.EventShellStarted, "client", c.config.name)

	sess := &sessionGo{
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		output:  &outputCapture{},
		events:  c.events,
		name:    c.config.name,
		cols:    cols,
		rows:    rows,
	}

	// Start capturing output
	go sess.captureOutput()

	return sess, nil
}

// Exec executes a command and returns the result.
func (c *clientGo) Exec(cmd string) (*ExecResult, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	c.events.Emit(scenario.EventExecStarted, "client", c.config.name, "command", cmd)

	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			return nil, fmt.Errorf("failed to run command: %w", err)
		}
	}

	c.events.Emit(scenario.EventExecCompleted, "client", c.config.name, "command", cmd, "exit_code", fmt.Sprint(result.ExitCode))

	return result, nil
}
