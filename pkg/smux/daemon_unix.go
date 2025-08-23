//go:build !windows

package smux

import (
	"os/exec"
	"syscall"
)

func setupDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}