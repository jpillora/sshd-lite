package sshd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func (s *Server) computeSSHConfig() (*ssh.ServerConfig, error) {
	sc := &ssh.ServerConfig{}
	if s.config.Shell == "" {
		if runtime.GOOS == "windows" {
			s.config.Shell = "powershell"
		} else {
			s.config.Shell = "bash"
		}
	}
	p, err := exec.LookPath(s.config.Shell)
	if err != nil {
		return nil, fmt.Errorf("failed to find shell: %s", s.config.Shell)
	}
	s.config.Shell = p
	s.debugf("Session shell %s", s.config.Shell)

	var key []byte
	if s.config.KeyFile != "" {
		//user provided key (can generate with 'ssh-keygen -t rsa')
		b, err := os.ReadFile(s.config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load keyfile")
		}
		key = b
	} else {
		//generate key now
		b, err := generateKey(s.config.KeySeed)
		if err != nil {
			return nil, fmt.Errorf("failed to generate private key")
		}
		key = b
	}
	pri, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key")
	}
	if s.config.KeyFile != "" {
		log.Printf("Key from file %s", s.config.KeyFile)
	} else if s.config.KeySeed == "" {
		log.Printf("Key from system rng")
	} else {
		log.Printf("Key from seed")
	}

	sc.AddHostKey(pri)
	log.Printf("RSA key fingerprint is %s", fingerprint(pri.PublicKey()))

	//setup auth
	if s.config.AuthType == "none" {
		sc.NoClientAuth = true // very dangerous
		log.Printf("Authentication disabled")
	} else if strings.HasPrefix(s.config.AuthType, "github.com/") {
		username := strings.TrimPrefix(s.config.AuthType, "github.com/")
		if err := s.githubCallback(username, sc); err != nil {
			return nil, err
		}
	} else if strings.Contains(s.config.AuthType, ":") {
		pair := strings.SplitN(s.config.AuthType, ":", 2)
		u := pair[0]
		p := pair[1]
		sc.PasswordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if conn.User() == u && string(pass) == p {
				s.debugf("User '%s' authenticated with password", u)
				return nil, nil
			}
			s.debugf("Authentication failed '%s:%s'", conn.User(), pass)
			return nil, fmt.Errorf("denied")
		}
		log.Printf("Authentication enabled (user '%s')", u)
	} else if s.config.AuthType != "" {
		if err := s.fileCallback(sc); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("missing auth-type")
	}
	return sc, nil
}

func (s *Server) githubCallback(username string, sc *ssh.ServerConfig) error {
	log.Printf("Fetching ssh public keys for github user %s", username)
	keys, err := githubKeys(username)
	if err != nil {
		return err
	}
	sc.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, s.matchKeys(key, keys)
	}
	log.Printf("Authentication enabled (github keys #%d)", len(keys))
	return nil
}

func (s *Server) fileCallback(sc *ssh.ServerConfig) error {
	//initial key parse
	keys, last, err := s.loadAuthTypeFile(time.Time{})
	if err != nil {
		return err
	}
	//setup checker
	sc.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		//update keys
		if ks, t, err := s.loadAuthTypeFile(last); err == nil {
			keys = ks
			last = t
			s.debugf("Updated authorized keys")
		}
		return nil, s.matchKeys(key, keys)
	}
	log.Printf("Authentication enabled (public keys #%d)", len(keys))
	return nil
}

func (s *Server) matchKeys(key ssh.PublicKey, keys map[string]string) error {
	if cmt, exists := keys[string(key.Marshal())]; exists {
		s.debugf("User '%s' authenticated with public key %s", cmt, fingerprint(key))
		return nil
	}
	s.debugf("User authentication failed with public key %s", fingerprint(key))
	return fmt.Errorf("denied")
}
