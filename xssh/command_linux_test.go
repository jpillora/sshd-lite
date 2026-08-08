//go:build linux

package xssh

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestWaitCommandKillsLeaderOutsideExpectedGroup(t *testing.T) {
	const helperEnv = "SSHD_LITE_MOVED_LEADER_HELPER"
	if os.Getenv(helperEnv) == "1" {
		time.Sleep(30 * time.Second)
		return
	}

	parentGroup := syscall.Getpgrp()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWaitCommandKillsLeaderOutsideExpectedGroup$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	// Put the child directly into an existing permissible group. There is no
	// group whose ID equals the child's PID, so group-only termination cannot
	// reach it and the mandatory direct signal is exercised.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: parentGroup}
	cmd.WaitDelay = commandWaitDelay
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	group, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("get helper process group: %v", err)
	}
	if group != parentGroup || group == cmd.Process.Pid {
		t.Fatalf("helper group = %d, parent group = %d, helper pid = %d", group, parentGroup, cmd.Process.Pid)
	}

	done := make(chan struct{})
	close(done)
	started := time.Now()
	waitErr, lifecycleErr := waitCommand(cmd, done)
	if lifecycleErr != nil {
		t.Fatalf("terminate helper: %v", lifecycleErr)
	}
	if waitErr == nil {
		t.Fatal("terminated helper returned successful status")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("waitCommand took %s after cancellation", elapsed)
	}
	if cmd.ProcessState == nil {
		t.Fatalf("helper was not reaped: %#v", cmd.ProcessState)
	}
}
