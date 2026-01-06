package sshd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/jpillora/sshd-lite/server/key"
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

	var keyBytes []byte
	if len(s.config.KeyBytes) > 0 {
		//user provided key bytes
		keyBytes = s.config.KeyBytes
	} else if s.config.KeyFile != "" {
		//user provided key (can generate with 'ssh-keygen')
		b, err := os.ReadFile(s.config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load keyfile")
		}
		keyBytes = b
	} else {
		//generate key now
		b, err := key.GenerateKey(s.config.KeySeed, s.config.KeySeedEC)
		if err != nil {
			return nil, fmt.Errorf("failed to generate private key")
		}
		keyBytes = b
	}
	pri, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key")
	}
	sc.AddHostKey(pri)
	s.infof("Private key loaded")
	s.infof("Public Key: %s", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pri.PublicKey()))))
	s.infof("Fingerprint: %s", key.Fingerprint(pri.PublicKey()))
	//setup auth - if AuthKeys are set, use them exclusively
	if len(s.config.AuthKeys) > 0 {
		if s.config.AuthType != "" {
			return nil, fmt.Errorf("cannot use AuthType with AuthKeys")
		}
		sc.PublicKeyCallback = func(conn ssh.ConnMetadata, pubkey ssh.PublicKey) (*ssh.Permissions, error) {
			for _, k := range s.config.AuthKeys {
				if bytes.Equal(pubkey.Marshal(), k.Marshal()) {
					s.debugf("User authenticated with public key %s", key.Fingerprint(pubkey))
					return nil, nil
				}
			}
			s.debugf("User authentication failed with public key %s", key.Fingerprint(pubkey))
			return nil, fmt.Errorf("denied")
		}
		s.infof("Authentication enabled (auth keys #%d)", len(s.config.AuthKeys))
	} else if s.config.AuthType == "none" {
		sc.NoClientAuth = true // very dangerous
		s.infof("Authentication disabled")
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
		s.infof("Authentication enabled (user '%s')", u)
	} else if s.config.AuthType != "" {
		if err := s.fileCallback(sc); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("missing key authorization configuration")
	}
	return sc, nil
}

func (s *Server) githubCallback(username string, sc *ssh.ServerConfig) error {
	s.infof("Fetching ssh public keys for github user %s", username)
	keys, err := key.GitHubKeys(username)
	if err != nil {
		return err
	}
	sc.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, s.matchKeys(key, keys)
	}
	s.infof("Authentication enabled (github keys #%d)", len(keys))
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
	s.infof("Authentication enabled (public keys #%d)", len(keys))
	return nil
}

func (s *Server) matchKeys(pubkey ssh.PublicKey, keys key.Map) error {
	cmt, ok := keys[string(pubkey.Marshal())]
	if ok {
		s.debugf("User '%s' authenticated with public key %s", cmt, key.Fingerprint(pubkey))
		return nil
	}
	s.debugf("User authentication failed with public key %s", key.Fingerprint(pubkey))
	return fmt.Errorf("denied")
}
