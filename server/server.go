package gosshpot 

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
)

//Server is a simple SSH Daemon
type Server struct {
	c  *Config
	sc *ssh.ServerConfig
}

func ServerString(s string) (string) {
	return s
}



//NewServer creates a new Server
func NewServer(c *Config) (*Server, error) {

	var banner string = "Unauthorized access or use is forbidden.\nThis system is monitored for administrative and security reasons.\nThis server is used to fuzz test client software, and will send lengthy and/or unexpected data in response to client input.\nBy proceeding, you acknowledge that (1) you have read and understand this notice, (2) you consent to the system monitoring, and (3) you consent to your software configuration being tested, and waive any claims against any and all persons for damage to your system as a result of participating in the test.\n"

	sc := &ssh.ServerConfig{}
	sc.MaxAuthTries = 3
	sc.BannerCallback = func(c ssh.ConnMetadata) (string) {
		return banner }

	s := &Server{c: c, sc: sc}

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

		sc.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			s.debugf("Attempt %s:%s:%s:%s", c.RemoteAddr(),c.User(), pass, c.ClientVersion())
			return nil, fmt.Errorf("denied")
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
		ssh.NewServerConn(tcpConn, s.sc)

	}
}


func (s *Server) debugf(f string, args ...interface{}) {
	if s.c.LogVerbose {
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
