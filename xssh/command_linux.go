//go:build linux

package xssh

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func setCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// waitCommand observes process exit without reaping it. Consequently the
// leader's PID (which is also the PGID) cannot be reused before a disconnect's
// group signal has completed. cmd.Wait remains the sole reaper.
func waitCommand(cmd *exec.Cmd, done <-chan struct{}) (error, error) {
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		for {
			err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if !errors.Is(err, syscall.EINTR) {
				exited <- err
				return
			}
		}
	}()

	select {
	case observeErr := <-exited:
		// Waitid with WNOWAIT reported exit but deliberately left reaping to
		// cmd.Wait. Clean up any descendants still in the command group before
		// reaping the leader; this also closes the race where teardown arrives
		// concurrently with leader exit.
		cleanupErr := terminateLinuxCommand(cmd)
		return cmd.Wait(), errors.Join(observeErr, cleanupErr)
	case <-done:
		// The leader is still an unreaped child even if it exited concurrently,
		// so this PGID cannot have been recycled for an unrelated process group.
		cleanupErr := terminateLinuxCommand(cmd)
		return cmd.Wait(), cleanupErr
	}
}

func terminateLinuxCommand(cmd *exec.Cmd) error {
	groupErr := killCommandGroup(cmd)
	// Always follow group signaling with os.Process's state-protected direct
	// signal. This covers a leader that is not in the expected group while the
	// unreaped-child invariant still prevents PID reuse.
	directErr := cmd.Process.Kill()
	if processAlreadyDone(directErr) {
		directErr = nil
	}
	return errors.Join(groupErr, directErr)
}

func killCommandGroup(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
