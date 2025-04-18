package sshd

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Server is a simple SSH Daemon
type Server struct {
	cli    *Config
	config *ssh.ServerConfig
}

// NewServer creates a new Server
func NewServer(c *Config) (*Server, error) {
	s := &Server{cli: c}
	sc, err := s.computeSSHConfig()
	if err != nil {
		return nil, err
	}
	s.config = sc
	return s, nil
}

// Start listening on port
func (s *Server) Start() error {
	h := s.cli.Host
	p := s.cli.Port
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
			return fmt.Errorf("failed to listen on " + p)
		}
	}
	if s.cli.SFTP {
		log.Print("SFTP enabled")
	}
	// Accept all connections
	log.Printf("Listening on %s:%s...", h, p)
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}
		go s.handleConn(tcpConn)
	}
}

func (s *Server) handleConn(tcpConn net.Conn) {
	// Before use, a handshake must be performed on the incoming net.Conn.
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, s.config)
	if err != nil {
		if err != io.EOF {
			log.Printf("Failed to handshake (%s)", err)
		}
		return
	}
	s.debugf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())
	// Discard all global out-of-band Requests
	go ssh.DiscardRequests(reqs)
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
	if t := newChannel.ChannelType(); t != "session" {
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		return
	}

	s.debugf("Channel request '%s'", newChannel.ChannelType())
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
}

func (s *Server) handleRequests(connection ssh.Channel, requests <-chan *ssh.Request) {
	// start keep alive loop
	if ka := s.cli.KeepAlive; ka > 0 {
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
			if !s.cli.IgnoreEnv {
				env = appendEnv(env, kv)
			}
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
			s.debugf("exec ignored '%s'", req.Payload)
		case "subsystem":
			s.handleSubsystemRequest(connection, req)
		default:
			s.debugf("unkown request: %s (reply: %v, data: %x)", req.Type, req.WantReply, req.Payload)
		}
	}
	s.debugf("Closing handler for requests")
}

// handleSubsystemRequest handles 'subsystem' requests from the client.
func (s *Server) handleSubsystemRequest(connection ssh.Channel, req *ssh.Request) {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// subsystem name is a string encoded as: [uint32 length][string name]
	if len(req.Payload) < 4 {
		s.debugf("Malformed subsystem request payload")
		req.Reply(false, nil)
		return
	}
	length := binary.BigEndian.Uint32(req.Payload)
	match := uint32(len(req.Payload)-4) == length
	if !match {
		s.debugf("Subsystem name length mismatch in payload")
		req.Reply(false, nil)
		return
	}
	subsystem := string(req.Payload[4:])
	if subsystem == "sftp" {
		if !s.cli.SFTP { // Check if SFTP is enabled in config
			s.debugf("SFTP subsystem request received but SFTP is disabled")
			req.Reply(false, []byte("SFTP is disabled on this server"))
			return
		}
		s.debugf("SFTP subsystem request accepted")
		req.Reply(true, nil) // Acknowledge the request
		go s.startSFTPServer(connection)
	} else {
		s.debugf("Unsupported subsystem requested: %q", subsystem)
		req.Reply(false, nil) // Reject unsupported subsystems
		connection.Close()
	}
}

// startSFTPServer starts the SFTP server for the given connection.
func (s *Server) startSFTPServer(connection ssh.Channel) {
	defer connection.Close()
	opts := []sftp.ServerOption{}
	if s.cli.LogVerbose {
		opts = append(opts, sftp.WithDebug(os.Stderr))
	}
	sftpServer, err := sftp.NewServer(
		connection,
		opts...,
	)
	if err != nil {
		s.debugf("Failed to create SFTP server: %v", err)
		return
	}
	if err := sftpServer.Serve(); err != nil && err != io.EOF {
		s.debugf("SFTP request error: %s", err)
	} else {
		s.debugf("SFTP request served")
	}
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

func (s *Server) attachShell(connection ssh.Channel, env []string, resizes <-chan []byte) error {
	shell := exec.Command(s.cli.Shell)
	shell.Env = env
	s.debugf("Session env: %v", env)

	close := func() {
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
	info, err := os.Stat(s.cli.AuthType)
	if err != nil {
		return nil, last, fmt.Errorf("missing auth keys file")
	}
	t := info.ModTime()
	if t.Before(last) || t == last {
		return nil, last, fmt.Errorf("not updated")
	}
	b, _ := ioutil.ReadFile(s.cli.AuthType)
	keys, err := parseKeys(b)
	if err != nil {
		return nil, last, err
	}
	return keys, t, nil
}

func (s *Server) debugf(f string, args ...interface{}) {
	if s.cli.LogVerbose {
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
