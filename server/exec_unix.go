// +build !windows

package sshd

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr configures the command to run in a new process group on Unix systems.
// This allows the process to be killed independently from the parent process.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
