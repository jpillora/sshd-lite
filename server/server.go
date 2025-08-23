package sshd

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// Server is a simple SSH Daemon
type Server struct {
	config               *Config
	sshConfig            *ssh.ServerConfig
	tcpForwardingHandler *TCPForwardingHandler
	sessionManager       *SessionManager
}

// NewServer creates a new Server
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		config:         c,
		sessionManager: NewSessionManager(),
	}
	sc, err := s.computeSSHConfig()
	if err != nil {
		return nil, err
	}
	s.sshConfig = sc

	// Initialize TCP forwarding handler if enabled
	if c.TCPForwarding {
		s.tcpForwardingHandler = NewTCPForwardingHandler(s)
	}

	return s, nil
}

// Start listening on port
func (s *Server) Start() error {
	h := s.config.Host
	p := s.config.Port
	var l net.Listener
	var err error

	//listen
	if p == "" {
		p = "22"
		l, err = net.Listen("tcp", h+":22")
		if err != nil {
			p = "2200"
			l, err = net.Listen("tcp", h+":2200")
			if err != nil {
				return fmt.Errorf("failed to listen on 22 and 2200")
			}
		}
	} else {
		l, err = net.Listen("tcp", h+":"+p)
		if err != nil {
			return fmt.Errorf("failed to listen on %s", p)
		}
	}

	return s.StartWith(l)
}

// StartWith starts the server with the provided listener.
// Ignores the Host and Port in the config.
func (s *Server) StartWith(l net.Listener) error {
	defer l.Close()

	if s.config.SFTP {
		log.Print("SFTP enabled")
	}
	if s.config.TCPForwarding {
		log.Print("TCP forwarding enabled")
	}

	// Accept all connections
	log.Printf("Listening on %s...", l.Addr())
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			// Check if the error is due to listener being closed
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil // Expected error when stopping
			}
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}
		go s.handleConn(tcpConn)
	}
}

func (s *Server) handleConn(tcpConn net.Conn) {
	// Before use, a handshake must be performed on the incoming net.Conn.
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, s.sshConfig)
	if err != nil {
		if err != io.EOF {
			log.Printf("Failed to handshake (%s)", err)
		}
		return
	}
	s.debugf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

	// Handle global requests (for TCP forwarding)
	if s.config.TCPForwarding && s.tcpForwardingHandler != nil {
		go s.handleGlobalRequests(reqs, sshConn)
	} else {
		// Discard all global out-of-band Requests if TCP forwarding is disabled
		go ssh.DiscardRequests(reqs)
	}

	// Accept all channels
	go s.handleChannels(chans)
}

func (s *Server) handleChannels(chans <-chan ssh.NewChannel) {
	// Service the incoming Channel channel in go routine
	for newChannel := range chans {
		go s.handleChannel(newChannel)
	}
}

func (s *Server) handleChannel(newChannel ssh.NewChannel) {
	channelType := newChannel.ChannelType()
	s.debugf("Channel request '%s'", channelType)

	switch channelType {
	case "session":
		// Handle regular SSH sessions
		if d := newChannel.ExtraData(); len(d) > 0 {
			s.debugf("Channel data: '%s' %x", d, d)
		}

		connection, requests, err := newChannel.Accept()
		if err != nil {
			s.debugf("Could not accept channel (%s)", err)
			return
		}
		s.debugf("Channel accepted")
		go s.handleRequests(connection, requests)

	case "direct-tcpip":
		// Handle direct TCP/IP forwarding (local forwarding)
		if s.config.TCPForwarding && s.tcpForwardingHandler != nil {
			go s.tcpForwardingHandler.HandleDirectTCPIP(newChannel)
		} else {
			s.debugf("direct-tcpip request received but TCP forwarding is disabled")
			newChannel.Reject(ssh.Prohibited, "TCP forwarding is disabled")
		}

	default:
		s.debugf("Unknown channel type: %s", channelType)
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType))
	}
}

func (s *Server) handleRequests(connection ssh.Channel, requests <-chan *ssh.Request) {
	// start keep alive loop
	if ka := s.config.KeepAlive; ka > 0 {
		ticking := make(chan bool, 1)
		interval := time.Duration(ka) * time.Second
		go s.keepAlive(connection, interval, ticking)
		defer close(ticking)
	}
	// prepare to handle client requests
	env := os.Environ()
	resizes := make(chan []byte, 10)
	defer close(resizes)
	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	for req := range requests {
		s.debugf("Request type: %s", req.Type)
		switch req.Type {
		case "pty-req":
			termLen := req.Payload[3]
			resizes <- req.Payload[termLen+4:]
			// Responding true (OK) here will let the client
			// know we have a pty ready
			s.debugf("pty ready")
			req.Reply(true, nil)
		case "window-change":
			resizes <- req.Payload
		case "env":
			e := struct{ Name, Value string }{}
			ssh.Unmarshal(req.Payload, &e)
			kv := e.Name + "=" + e.Value
			s.debugf("env: %s", kv)
			if !s.config.IgnoreEnv {
				env = appendEnv(env, kv)
			}
		case "list":
			data, err := s.sessionManager.GetSessionsJSON()
			req.Reply(err == nil, data)
		case "shell":
			// Responding true (OK) here will let the client
			// know we have attached the shell (pty) to the connection
			if len(req.Payload) > 0 {
				s.debugf("shell command ignored '%s'", req.Payload)
			}
			err := s.attachShell(connection, env, resizes)
			if err != nil {
				s.debugf("exec shell: %s", err)
			}
			req.Reply(err == nil, nil)
		case "exec":
			ok := s.handleExecRequest(connection, req, env)
			req.Reply(ok, nil)
		case "subsystem":
			ok := s.handleSubsystemRequest(connection, req)
			if !ok {
				req.Reply(false, nil) // Reject unsupported subsystems
				connection.Close()
			}
		default:
			s.debugf("unkown request: %s (reply: %v, data: %x)", req.Type, req.WantReply, req.Payload)
		}
	}
	s.debugf("Closing handler for requests")
}

// handleSubsystemRequest handles 'subsystem' requests from the client.
func (s *Server) handleSubsystemRequest(connection ssh.Channel, req *ssh.Request) bool {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// subsystem name is a string encoded as: [uint32 length][string name]
	if len(req.Payload) < 4 {
		s.debugf("Malformed subsystem request payload")
		return false
	}
	length := binary.BigEndian.Uint32(req.Payload)
	match := uint32(len(req.Payload)-4) == length
	if !match {
		s.debugf("Subsystem name length mismatch in payload")
		return false
	}
	subsystem := string(req.Payload[4:])
	switch subsystem {
	case "sftp":
		return s.handleSFTP(connection, req)
	default:
		s.debugf("Unsupported subsystem requested: %q", subsystem)
		return false
	}
}

func (s *Server) handleSFTP(connection ssh.Channel, _ *ssh.Request) bool {
	if !s.config.SFTP { // Check if SFTP is enabled in config
		s.debugf("SFTP subsystem request received but SFTP is disabled")
		return false
	}
	// req.Reply(false, []byte("SFTP is disabled on this server"))
	s.debugf("SFTP subsystem request accepted")
	go s.startSFTPServer(connection)
	return true
}

func (s *Server) keepAlive(connection ssh.Channel, interval time.Duration, ticking <-chan bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err := connection.SendRequest("ping", false, nil)
			if err != nil {
				s.debugf("failed to send keep alive ping: %s", err)
			}
			s.debugf("sent keep alive ping")
		case <-ticking:
			return
		}
	}
}

func generateSessionID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (s *Server) attachShell(connection ssh.Channel, env []string, resizes <-chan []byte) error {
	sessionID := generateSessionID()
	
	shell := exec.Command("/bin/bash")
	shell.Env = env
	s.debugf("Session env: %v", env)

	close := func() {
		s.sessionManager.RemoveSession(sessionID)
		connection.Close()
		if shell.Process != nil {
			// Use Signal instead of Wait to avoid blocking if process already exited
			err := shell.Process.Signal(os.Interrupt)
			if err != nil && !strings.Contains(err.Error(), "process already finished") && !strings.Contains(err.Error(), "already exited") {
				log.Printf("Failed to interrupt shell: %s", err)
			}
			// Give a short time for the process to exit gracefully before killing
			time.Sleep(100 * time.Millisecond)
			shell.Process.Kill() // Ensure process is killed
			shell.Process.Wait() // Wait for cleanup
		}
		s.debugf("Session closed")
	}
	//start a shell for this channel's connection
	shellf, err := pty.Start(shell)
	if err != nil {
		close()
		return fmt.Errorf("could not start pty (%s)", err)
	}
	
	// Add session to session manager
	if shell.Process != nil {
		s.sessionManager.AddSession(sessionID, "bash", shell.Process.Pid)
	}
	//dequeue resizes
	go func() {
		for payload := range resizes {
			w, h := parseDims(payload)
			SetWinsize(shellf, w, h)
		}
	}()
	//pipe session to shell and visa-versa
	var once sync.Once
	go func() {
		io.Copy(connection, shellf)
		once.Do(close)
	}()
	go func() {
		io.Copy(shellf, connection)
		once.Do(close)
	}()
	//
	s.debugf("shell attached")
	go func() {
		// Start proactively listening for process death, for those ptys that
		// don't signal on EOF.
		if shell.Process != nil {
			if ps, err := shell.Process.Wait(); err != nil && ps != nil && !strings.Contains(err.Error(), "wait: no child processes") && !strings.Contains(err.Error(), "exit status") && !strings.Contains(err.Error(), "Wait was already called") {
				log.Printf("Shell process wait error: (%s)", err)
			}
			// It appears that closing the pty is an idempotent operation
			// therefore making this call ensures that the other two coroutines
			// will fall through and exit, and there is no downside.
			shellf.Close() // Close the pty file descriptor
		}
		s.debugf("Shell terminated")
		once.Do(close) // Ensure connection is closed when shell exits
	}()
	return nil
}

func (s *Server) loadAuthTypeFile(last time.Time) (map[string]string, time.Time, error) {
	info, err := os.Stat(s.config.AuthType)
	if err != nil {
		return nil, last, fmt.Errorf("missing auth keys file")
	}
	t := info.ModTime()
	if t.Before(last) || t.Equal(last) {
		return nil, last, fmt.Errorf("not updated")
	}
	b, _ := os.ReadFile(s.config.AuthType)
	keys, err := parseKeys(b)
	if err != nil {
		return nil, last, err
	}
	return keys, t, nil
}

func (s *Server) debugf(f string, args ...interface{}) {
	if s.config.LogVerbose {
		log.Printf(f, args...)
	}
}

func appendEnv(env []string, kv string) []string {
	p := strings.SplitN(kv, "=", 2)
	k := p[0] + "="
	for i, e := range env {
		if strings.HasPrefix(e, k) {
			env[i] = kv
			return env
		}
	}
	return append(env, kv)
}

func (s *Server) handleGlobalRequests(reqs <-chan *ssh.Request, conn ssh.Conn) {
	for req := range reqs {
		s.debugf("Global request: %s", req.Type)
		if s.tcpForwardingHandler != nil {
			s.tcpForwardingHandler.HandleGlobalRequest(req, conn)
		} else {
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// handleExecRequest handles 'exec' requests from the client.
func (s *Server) handleExecRequest(connection ssh.Channel, req *ssh.Request, env []string) bool {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// command name is a string encoded as: [uint32 length][string command]
	if len(req.Payload) < 4 {
		s.debugf("Malformed exec request payload")
		return false
	}
	length := binary.BigEndian.Uint32(req.Payload)
	if uint32(len(req.Payload)-4) != length {
		s.debugf("Command length mismatch in payload")
		return false
	}
	command := string(req.Payload[4:])
	s.debugf("exec command: %s", command)

	// Execute the command
	go s.executeCommand(connection, command, env)
	return true
}

// executeCommand executes a shell command and pipes the output to the SSH connection
func (s *Server) executeCommand(connection ssh.Channel, command string, env []string) {
	defer connection.Close()
	// Use shell to execute the command
	cmd := exec.Command(s.config.Shell, "-c", command)
	cmd.Env = env          // TODO: append?
	cmd.Stdin = connection // Connect stdin to the SSH channel
	cmd.Stdout = connection
	cmd.Stderr = connection
	// capture exit status
	type exit struct {
		Status uint32
	}
	status := exit{Status: 0}
	// Run the command
	err := cmd.Run()
	if err != nil {
		s.debugf("Command execution failed: %s", err)
		// Send exit status
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Send exit status to client
			status := exit{Status: uint32(exitErr.ExitCode())}
			connection.SendRequest("exit-status", false, ssh.Marshal(&status))
		}
	}
	s.debugf("Command execution completed")
	connection.SendRequest("exit-status", false, ssh.Marshal(&status))
}
