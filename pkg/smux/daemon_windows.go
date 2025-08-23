//go:build windows

package smux

import "os/exec"

func setupDaemonProcess(cmd *exec.Cmd) {
	// Windows doesn't support Setsid, use CREATE_NEW_PROCESS_GROUP instead
	// This is handled automatically by Go on Windows for background processes
}