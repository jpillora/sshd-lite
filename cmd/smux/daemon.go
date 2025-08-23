package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/jpillora/sshd-lite/server"
)

const (
	defaultSocketPath = "/var/run/smux.sock"
	defaultPIDPath    = "/var/run/smux.pid"
	defaultLogPath    = "/var/run/smux.log"
)

func isDaemonRunning() bool {
	pidBytes, err := os.ReadFile(defaultPIDPath)
	if err != nil {
		return false
	}
	
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return false
	}
	
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func startDaemonBackground() error {
	if isDaemonRunning() {
		return fmt.Errorf("daemon already running")
	}
	
	cmd := exec.Command(os.Args[0], "daemon", "--foreground")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	
	return cmd.Start()
}

func runDaemonProcess(foreground bool) error {
	if !foreground {
		logFile, err := os.OpenFile(defaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	
	err := os.WriteFile(defaultPIDPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %v", err)
	}
	defer os.Remove(defaultPIDPath)
	
	os.Remove(defaultSocketPath)
	
	config := &sshd.Config{
		Shell:     "/bin/bash",
		SFTP:      false,
		AuthType:  "password",
		IgnoreEnv: true,
	}
	
	server, err := sshd.NewServer(config)
	if err != nil {
		return fmt.Errorf("failed to create server: %v", err)
	}
	
	log.Printf("Starting daemon on %s", defaultSocketPath)
	return server.StartUnixSocket(defaultSocketPath)
}