//go:build !linux && !windows

package xssh

import "os/exec"

func setCommandProcessGroup(cmd *exec.Cmd) {
	// Safe process-tree termination is only enabled where exit can be observed
	// without reaping the process-group leader. Other Unix platforms use the
	// state-protected direct-process fallback in waitCommand.
}
