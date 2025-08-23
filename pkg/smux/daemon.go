package smux

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
)

const (
	DefaultSocketPath = "/var/run/smux.sock"
	DefaultPIDPath    = "/var/run/smux.pid"
	DefaultLogPath    = "/var/run/smux.log"
)

func getPIDPath() string {
	if isWritable("/var/run/") {
		return DefaultPIDPath
	}
	return "/tmp/smux.pid"
}

func getLogPath() string {
	if isWritable("/var/run/") {
		return DefaultLogPath
	}
	return "/tmp/smux.log"
}

func isWritable(path string) bool {
	// Test if we can create a file in the directory
	testFile := path + ".smux_test"
	file, err := os.Create(testFile)
	if err != nil {
		return false
	}
	file.Close()
	os.Remove(testFile)
	return true
}

type Daemon struct {
	sessionManager *SessionManager
	httpServer     *HTTPServer
	mu             sync.Mutex
}

func NewDaemon() *Daemon {
	sessionManager := NewSessionManager()
	httpServer := NewHTTPServer(sessionManager)
	
	return &Daemon{
		sessionManager: sessionManager,
		httpServer:     httpServer,
	}
}

func IsDaemonRunning() bool {
	pidPath := getPIDPath()
	pidBytes, err := os.ReadFile(pidPath)
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
	pidPath := getPIDPath()
	
	if !foreground {
		logPath := getLogPath()
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	
	err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %v", err)
	}
	defer os.Remove(pidPath)
	
	daemon := NewDaemon()
	
	// Create a default session
	log.Println("Creating default session")
	daemon.sessionManager.CreateSession("")
	
	log.Printf("Starting HTTP server on port %d", HTTPPort)
	return daemon.httpServer.Start()
}