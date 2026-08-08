//go:build !linux

package xssh

import (
	"os/exec"
)

func waitCommand(cmd *exec.Cmd, done <-chan struct{}) (error, error) {
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		return err, nil
	case <-done:
		// os.Process.Kill synchronizes with Process.Wait internally, preventing a
		// signal from targeting a process that has already been reaped and reused.
		killErr := cmd.Process.Kill()
		if processAlreadyDone(killErr) {
			killErr = nil
		}
		return <-waited, killErr
	}
}
