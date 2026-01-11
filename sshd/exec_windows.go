//go:build windows
// +build windows

package sshd

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr configures the command to run in a new process group on Windows.
// This allows the process to be killed independently from the parent process.
// Reference: https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
