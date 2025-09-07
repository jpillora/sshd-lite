package smux

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	DefaultSocketPath = "/var/run/smux.sock"
	DefaultPIDPath    = "/var/run/smux.pid"
	DefaultLogPath    = "/var/run/smux.log"
)

type Config struct {
	SocketPath string `opts:"help=Unix socket path for daemon"`
	PIDPath    string `opts:"help=PID file path"`
	LogPath    string `opts:"help=Log file path"`
	HTTPPort   int    `opts:"help=HTTP port for web interface"`
}

type Daemon struct {
	config         Config
	sessionManager *sessionManager
	httpServer     *httpServer
}

func NewDaemon(config Config) *Daemon {
	if config.SocketPath == "" {
		config.SocketPath = DefaultSocketPath
	}
	if config.PIDPath == "" {
		config.PIDPath = config.getPIDPath()
	}
	if config.LogPath == "" {
		config.LogPath = config.getLogPath()
	}
	if config.HTTPPort == 0 {
		config.HTTPPort = HTTPPort
	}

	sessionManager := newSessionManager()
	httpServer := newHTTPServer(sessionManager, config.HTTPPort)

	return &Daemon{
		config:         config,
		sessionManager: sessionManager,
		httpServer:     httpServer,
	}
}

func (c Config) getPIDPath() string {
	if c.isWritable("/var/run/") {
		return DefaultPIDPath
	}
	return "/tmp/smux.pid"
}

func (c Config) getLogPath() string {
	if c.isWritable("/var/run/") {
		return DefaultLogPath
	}
	return "/tmp/smux.log"
}

func (c Config) isWritable(path string) bool {
	testFile := path + ".smux_test"
	file, err := os.Create(testFile)
	if err != nil {
		return false
	}
	file.Close()
	os.Remove(testFile)
	return true
}

func (d *Daemon) IsRunning() bool {
	pidBytes, err := os.ReadFile(d.config.PIDPath)
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

func (d *Daemon) StartBackground() error {
	if d.IsRunning() {
		return fmt.Errorf("daemon already running")
	}

	cmd := exec.Command(os.Args[0], "daemon", "--background")
	d.setupDaemonProcess(cmd)

	return cmd.Start()
}

func (d *Daemon) Run(foreground bool) error {
	if !foreground {
		logFile, err := os.OpenFile(d.config.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	err := os.WriteFile(d.config.PIDPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %v", err)
	}
	defer os.Remove(d.config.PIDPath)

	log.Println("Creating default session")
	d.sessionManager.CreateSession("")

	log.Printf("Starting HTTP server on port %d", d.config.HTTPPort)
	return d.httpServer.Start()
}
