package xssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jpillora/sshd-lite/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// executeCommand executes a shell command and pipes the output to the SSH connection
func executeCommand(sess *Session, command string) {
	if h := sess.Config().ExecHandler; h != nil {
		handled, err := h(sess, command)
		if err != nil {
			commandSetupFailed(sess, "virtual command failed", err)
			return
		}
		if handled {
			return
		}
	}
	defer sess.Channel.Close()
	cfg := sess.Config()

	// Use shell to execute the command
	cmd := exec.Command(cfg.Shell, commandFlag(cfg.Shell), command)
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

// shellReapTimeout bounds how long session teardown waits to reap a shell that
// has already been interrupted and killed. Shutdown correctness does not depend
// on collecting the exit status, so this trades a possible zombie for a server
// that always stops.
const shellReapTimeout = 5 * time.Second

// shellOutputFlushTimeout bounds how long a reaped shell's exit status waits on
// its own output to finish forwarding. Reporting late is better than reporting
// out of order, but a stuck copy must not withhold the status indefinitely.
const shellOutputFlushTimeout = 2 * time.Second

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

// shellExitCode maps a reaped interactive shell onto the status reported to the
// client. ExitCode is -1 when the shell was signalled rather than exiting; that
// only reaches here if something outside this session killed it, since our own
// teardown path reports nothing at all.
func shellExitCode(state *os.ProcessState) uint32 { return uint32(terminal.ExitCode(state)) }

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
