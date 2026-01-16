//go:build !windows
// +build !windows

package xssh

import (
	"os/exec"
)

func setSysProcAttr(cmd *exec.Cmd) {
	// no special flags needed for unix-like systems
}
