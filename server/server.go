package sshd

import (
	"bytes"
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
	"golang.org/x/crypto/ssh"
)

//Server is a simple SSH Daemon
type Server struct {
	c  *Config
	sc *ssh.ServerConfig
}

//NewServer creates a new Server
func NewServer(c *Config) (*Server, error) {

	sc := &ssh.ServerConfig{}
	s := &Server{c: c, sc: sc}

	if c.Shell == "" {
		c.Shell = "bash"
	}
	p, err := exec.LookPath(c.Shell)
	if err != nil {
		return nil, fmt.Errorf("Failed to find shell: %s", c.Shell)
	}
	c.Shell = p
	s.debugf("Session shell %s", c.Shell)

	var key []byte
	if c.KeyFile != "" {
		//user provided key (can generate with 'ssh-keygen -t rsa')
		b, err := ioutil.ReadFile(c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("Failed to load keyfile")
		}
		key = b
	} else {
		//generate key now
		b, err := generateKey(c.KeySeed)
		if err != nil {
			return nil, fmt.Errorf("Failed to generate private key")
		}
		key = b
	}
	pri, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse private key")
	}
	if c.KeyFile != "" {
		log.Printf("Key from file %s", c.KeyFile)
	} else if c.KeySeed == "" {
		log.Printf("Key from system rng")
	} else {
		log.Printf("Key from seed")
	}

	sc.AddHostKey(pri)
	log.Printf("RSA key fingerprint is %s", fingerprint(pri.PublicKey()))

	//setup auth
	if c.AuthType == "none" {
		sc.NoClientAuth = true // very dangerous
		log.Printf("Authentication disabled")
	} else if strings.Contains(c.AuthType, ":") {
		pair := strings.SplitN(c.AuthType, ":", 2)
		u := pair[0]
		p := pair[1]
		sc.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == u && string(pass) == p {
				s.debugf("User '%s' authenticated with password", u)
				return nil, nil
			}
			s.debugf("Authentication failed '%s:%s'", c.User(), pass)
			return nil, fmt.Errorf("denied")
		}
		log.Printf("Authentication enabled (user '%s')", u)
	} else if c.AuthType != "" {
		//initial key parse
		keys, last, err := s.parseAuth(time.Time{})
		if err != nil {
			return nil, err
		}
		//setup checker
		sc.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			//update keys
			if ks, t, err := s.parseAuth(last); err == nil {
				keys = ks
				last = t
				s.debugf("Updated authorized keys")
			}
			k := string(key.Marshal())
			if cmt, exists := keys[k]; exists {
				s.debugf("User '%s' authenticated with public key %s", cmt, fingerprint(key))
				return nil, nil
			}
			s.debugf("User authentication failed with public key %s", fingerprint(key))
			return nil, fmt.Errorf("denied")
		}
		log.Printf("Authentication enabled (public keys #%d)", len(keys))
	} else {
		return nil, fmt.Errorf("Missing auth-type")
	}
	return s, nil
}

//Start listening on port
func (s *Server) Start() error {
	h := s.c.Host
	p := s.c.Port
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
				return fmt.Errorf("Failed to listen on 22 and 2200")
			}
		}
	} else {
		l, err = net.Listen("tcp", h+":"+p)
		if err != nil {
			return fmt.Errorf("Failed to listen on " + p)
		}
	}

	// Accept all connections
	log.Printf("Listening on %s:%s...", h, p)
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}
		// Before use, a handshake must be performed on the incoming net.Conn.
		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, s.sc)
		if err != nil {
			if err != io.EOF {
				log.Printf("Failed to handshake (%s)", err)
			}
			continue
		}

		s.debugf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())
		// Discard all global out-of-band Requests
		go ssh.DiscardRequests(reqs)
		// Accept all channels
		go s.handleChannels(chans)
	}
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
	env := os.Environ()
	resizes := make(chan []byte, 10)
	defer close(resizes)
	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	for req := range requests {
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
			if !s.c.IgnoreEnv {
				env = append(env, kv)
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
		default:
			s.debugf("unkown request: %s (reply: %v, data: %x)", req.Type, req.WantReply, req.Payload)
		}
	}
}

func (s *Server) attachShell(connection ssh.Channel, env []string, resizes <-chan []byte) error {

	shell := exec.Command(s.c.Shell)
	shell.Env = env

	close := func() {
		connection.Close()
		if shell.Process != nil {
			if _, err := shell.Process.Wait(); err != nil {
				log.Printf("Failed to exit shell (%s)", err)
			}
		}
		s.debugf("Session closed")
	}
	//start a shell for this channel's connection
	shellf, err := pty.Start(shell)
	if err != nil {
		close()
		return fmt.Errorf("Could not start pty (%s)", err)
	}
	//dequeue resizes
	go func() {
		for payload := range resizes {
			w, h := parseDims(payload)
			SetWinsize(shellf.Fd(), w, h)
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
	return nil
}

func (s *Server) parseAuth(last time.Time) (map[string]string, time.Time, error) {

	info, err := os.Stat(s.c.AuthType)
	if err != nil {
		return nil, last, fmt.Errorf("Missing auth keys file")
	}

	t := info.ModTime()
	if t.Before(last) || t == last {
		return nil, last, fmt.Errorf("Not updated")
	}

	//grab file
	b, _ := ioutil.ReadFile(s.c.AuthType)
	lines := bytes.Split(b, []byte("\n"))
	//parse each line
	keys := map[string]string{}
	for _, l := range lines {
		if key, cmt, _, _, err := ssh.ParseAuthorizedKey(l); err == nil {
			keys[string(key.Marshal())] = cmt
		}
	}
	//ensure we got something
	if len(keys) == 0 {
		return nil, last, fmt.Errorf("No keys found in %s", s.c.AuthType)
	}
	return keys, t, nil
}

func (s *Server) debugf(f string, args ...interface{}) {
	if s.c.LogVerbose {
		log.Printf(f, args...)
	}
}
