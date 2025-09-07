package smux

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"
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
	unixListener   net.Listener
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
	httpServer := newHTTPServer(sessionManager, config.HTTPPort, config.SocketPath)

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

	args := []string{"daemon", "--no-foreground"}
	if d.config.SocketPath != DefaultSocketPath {
		args = append(args, "--socket-path", d.config.SocketPath)
	}
	if d.config.PIDPath != "" && d.config.PIDPath != d.config.getPIDPath() {
		args = append(args, "--pid-path", d.config.PIDPath)
	}
	if d.config.LogPath != "" && d.config.LogPath != d.config.getLogPath() {
		args = append(args, "--log-path", d.config.LogPath)
	}
	if d.config.HTTPPort != 0 && d.config.HTTPPort != HTTPPort {
		args = append(args, "--http-port", fmt.Sprintf("%d", d.config.HTTPPort))
	}
	
	cmd := exec.Command(os.Args[0], args...)
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

	if err := d.startSSHServer(); err != nil {
		return fmt.Errorf("failed to start SSH server: %v", err)
	}
	defer d.stopSSHServer()

	log.Printf("Starting HTTP server on port %d", d.config.HTTPPort)
	return d.httpServer.Start()
}

func (d *Daemon) startSSHServer() error {
	os.Remove(d.config.SocketPath)
	
	listener, err := net.Listen("unix", d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket: %v", err)
	}
	d.unixListener = listener
	
	if err := os.Chmod(d.config.SocketPath, 0600); err != nil {
		return fmt.Errorf("failed to set socket permissions: %v", err)
	}
	
	go func() {
		log.Printf("SSH server listening on unix socket: %s", d.config.SocketPath)
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("SSH listener error: %v", err)
				return
			}
			go d.handleSSHConnection(conn)
		}
	}()
	
	return nil
}

func (d *Daemon) stopSSHServer() {
	if d.unixListener != nil {
		d.unixListener.Close()
		os.Remove(d.config.SocketPath)
	}
}

func (d *Daemon) generateSSHServerConfig() (*ssh.ServerConfig, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, err
	}

	config := &ssh.ServerConfig{
		NoClientAuth: true, // Allow any client to connect
	}
	config.AddHostKey(signer)

	return config, nil
}

func (d *Daemon) handleSSHConnection(conn net.Conn) {
	defer conn.Close()

	config, err := d.generateSSHServerConfig()
	if err != nil {
		log.Printf("Failed to generate SSH config: %v", err)
		return
	}

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		log.Printf("Failed to handshake SSH connection: %v", err)
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		go d.handleSSHSession(newChannel, sshConn.User())
	}
}

func (d *Daemon) handleSSHSession(newChannel ssh.NewChannel, username string) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	sessionName := d.extractSessionName(username)
	
	for req := range requests {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)
		case "shell":
			d.attachToSmuxSession(channel, sessionName)
			req.Reply(true, nil)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (d *Daemon) extractSessionName(username string) string {
	parts := strings.Split(username, "@")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "1" // default session
}

func (d *Daemon) attachToSmuxSession(channel ssh.Channel, sessionName string) {
	session, exists := d.sessionManager.GetSession(sessionName)
	if !exists {
		var err error
		session, err = d.sessionManager.CreateSession(sessionName)
		if err != nil {
			channel.Write([]byte(fmt.Sprintf("Failed to create session %s: %v\r\n", sessionName, err)))
			return
		}
		log.Printf("Created new session '%s' for SSH client", sessionName)
	} else {
		log.Printf("Attaching SSH client to existing session '%s'", sessionName)
	}

	go func() {
		io.Copy(channel, session.PTY)
	}()
	
	go func() {
		io.Copy(session.PTY, channel)
	}()
}
