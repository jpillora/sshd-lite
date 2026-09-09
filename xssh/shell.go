package xssh

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/sshd-lite/internal/terminal"
)

// attachShell attaches a shell to the session
func attachShell(sess *Session) error {
	cfg := sess.Config()
	shell := terminal.Command(cfg.Shell, nil)
	var process *terminal.Process
	if cfg.WorkingDirectory != "" {
		shell.Dir = cfg.WorkingDirectory
	}
	if !hasEnv(sess.Env, "TERM") {
		sess.Env = append(sess.Env, "TERM=xterm-256color")
	}
	shell.Env = sess.Env
	debugf(sess, "Session env: %v", sess.Env)

	// This adapter owns SSH channel closure and graceful interruption; the
	// internal terminal owner alone reaps the process and serializes PTY release.
	closeFunc := func() {
		sess.Channel.Close()
		if process != nil {
			signalErr := process.Signal(os.Interrupt)
			if signalErr != nil && !strings.Contains(signalErr.Error(), "process already finished") && !strings.Contains(signalErr.Error(), "already exited") && !strings.Contains(signalErr.Error(), "not supported") {
				errorf(sess, "Failed to interrupt shell: %s", signalErr)
			}
			time.Sleep(100 * time.Millisecond)
			killErr := process.Kill()
			if killErr != nil && !strings.Contains(killErr.Error(), "process already finished") && !strings.Contains(killErr.Error(), "already exited") && !strings.Contains(killErr.Error(), "not supported") {
				errorf(sess, "Failed to kill shell: %s", killErr)
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
	shellf, err := terminal.Start(shell, initialSize)
	if err != nil {
		closeFunc()
		return fmt.Errorf("could not start pty: %w", err)
	}

	process = shellf

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
			err = process.Resize(ws.Cols, ws.Rows)
			if err != nil {
				errorf(sess, "SetWinsize failed: %s", err)
			}
		}
	})

	// pipe session to shell and visa-versa
	var once sync.Once
	// Closed once the shell's output has been fully forwarded. The reaper waits
	// for this before reporting exit status, so a client cannot receive the
	// status ahead of the output that produced it.
	outputDone := make(chan struct{})
	sess.goTask(func() {
		_, err := io.Copy(sess.Channel, shellf)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Shell to connection copy error: %s", err)
		}
		// Teardown belongs to the reaper. Closing the channel here raced it and
		// won, which is why an interactive session never delivered exit-status
		// and every clean exit surfaced to the client as 255.
		close(outputDone)
	})
	sess.goTask(func() {
		_, err := io.Copy(shellf, sess.Channel)
		if err != nil && !strings.Contains(err.Error(), "file already closed") && !strings.Contains(err.Error(), "use of closed connection") {
			debugf(sess, "Connection to shell copy error: %s", err)
		}
		// EOF here means the client closed its write side, not that the session
		// is over — the same distinction copyCommandStdin makes for exec, and
		// the one Session.Done documents. Tearing the shell down here killed
		// every session whose client piped input and closed stdin. The shell is
		// reaped when it exits on its own, or by the Done path below when the
		// client actually goes away.
	})

	debugf(sess, "Shell attached")

	releasePTY := process.Release

	sess.goTask(func() {
		// Report process completion after draining output, or stop on disconnect.
		if process != nil {
			// The shared process owner has one reaper. Keep this adapter's bounded
			// shutdown policy so a stuck OS wait cannot hold the SSH server open.
			var state *os.ProcessState
			var waitErr error
			timedOut := false
			select {
			case <-process.Done():
				state, waitErr = process.Result()
				// The shell exited on its own terms, so the client is still
				// attached and expects its status. Release the pty first: on
				// unix the master only reports EOF once this side closes it, so
				// the output copy would otherwise never finish draining.
				releasePTY()
				select {
				case <-outputDone:
				case <-time.After(shellOutputFlushTimeout):
					debugf(sess, "Shell output still draining after %s; reporting exit status anyway", shellOutputFlushTimeout)
				}
				sendExitStatus(sess, shellExitCode(state))
			case <-sess.Done():
				// Make the shell actually go away before bounding the wait:
				// closeFunc interrupts it and then kills it.
				once.Do(closeFunc)
				// Release the PTY here too. Teardown has already stopped the
				// copy goroutine that drains the master, so a shell writing to
				// a full PTY stays blocked in the kernel and never acts on the
				// kill. Waiting for the reaper before closing the master would
				// therefore wait on a shell that only this close can release.
				releasePTY()
				select {
				case <-process.Done():
					state, waitErr = process.Result()
				case <-time.After(shellReapTimeout):
					timedOut = true
				}
			}
			err := waitErr
			if timedOut {
				errorf(sess, "Shell did not exit within %s of shutdown; abandoning its reaper", shellReapTimeout)
			} else if err != nil &&
				!strings.Contains(err.Error(), "wait: no child processes") &&
				!strings.Contains(err.Error(), "no child processes") &&
				!strings.Contains(err.Error(), "exit status") &&
				!strings.Contains(err.Error(), "Wait was already called") {
				errorf(sess, "Shell process wait error: %s", err)
			}
			releasePTY()
		}
		debugf(sess, "Shell terminated")
		once.Do(closeFunc)
	})

	return nil
}
