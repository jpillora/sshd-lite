//go:build !windows

package terminal

import "os/exec"

func prepare(cmd *exec.Cmd) {}
