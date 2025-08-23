package smux

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
	DefaultSocketPath = "/var/run/smux.sock"
	DefaultPIDPath    = "/var/run/smux.pid"
	DefaultLogPath    = "/var/run/smux.log"
)

func IsDaemonRunning() bool {
	pidBytes, err := os.ReadFile(DefaultPIDPath)
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

func StartDaemonBackground() error {
	if IsDaemonRunning() {
		return fmt.Errorf("daemon already running")
	}
	
	cmd := exec.Command(os.Args[0], "daemon", "--foreground")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	
	return cmd.Start()
}

func RunDaemonProcess(foreground bool) error {
	if !foreground {
		logFile, err := os.OpenFile(DefaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	
	err := os.WriteFile(DefaultPIDPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %v", err)
	}
	defer os.Remove(DefaultPIDPath)
	
	os.Remove(DefaultSocketPath)
	
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
	
	log.Printf("Starting daemon on %s", DefaultSocketPath)
	return server.StartUnixSocket(DefaultSocketPath)
}