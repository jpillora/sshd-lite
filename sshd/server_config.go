package sshd

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/xssh"
	"golang.org/x/crypto/ssh"
)

// maxAuthorizedKeysFileSize bounds per-authentication read and parse work while
// leaving room for thousands of ordinary authorized_keys entries.
const maxAuthorizedKeysFileSize = 1 << 20

var errAuthenticationDenied = errors.New("denied")

func (s *Server) computeSSHConfig() (*ssh.ServerConfig, error) {
	sc := &ssh.ServerConfig{}
	shellPath, err := xssh.ShellPath(s.config.Shell)
	if err != nil {
		return nil, err
	}
	s.config.Shell = shellPath
	s.debugf("Session shell %s", s.config.Shell)
	if s.config.WorkDir == "" {
		if w, err := os.Getwd(); err == nil {
			s.config.WorkDir = w
		}
	}
	s.infof("Work directory: %s", s.config.WorkDir)

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
	} else if u, p, ok := parseUserPass(s.config.AuthType); ok {
		sc.PasswordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if conn.User() == u && subtle.ConstantTimeCompare(pass, []byte(p)) == 1 {
				s.debugf("User '%s' authenticated with password", conn.User())
				return nil, nil
			}
			s.debugf("Password authentication failed for user '%s'", conn.User())
			return nil, errAuthenticationDenied
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

// parseUserPass splits an auth argument into a username and password pair.
//
// A drive-qualified Windows path such as "C:\keys\authorized_keys" also
// contains a colon, so it is deliberately never read as a credential pair.
// Getting that wrong is not a cosmetic mistake: it silently swaps public key
// authentication for password authentication with the drive letter as the user
// and the rest of the path as the password. Rejecting the pair here instead
// leaves the value to the file loader, which fails loudly when it cannot read
// the file. The rule is platform independent so the classification of a given
// argument does not change with the host operating system.
func parseUserPass(auth string) (string, string, bool) {
	if hasWindowsDriveLetter(auth) {
		return "", "", false
	}
	return strings.Cut(auth, ":")
}

func hasWindowsDriveLetter(auth string) bool {
	if len(auth) < 3 || auth[1] != ':' || (auth[2] != '\\' && auth[2] != '/') {
		return false
	}
	drive := auth[0]
	return ('a' <= drive && drive <= 'z') || ('A' <= drive && drive <= 'Z')
}

// Paths are delimited with single quotes rather than %q. A Windows path such as
// C:\keys\authorized_keys survives verbatim that way, so the operator sees the
// path they configured; %q would escape every separator. This also matches how
// the wrapped os errors in these messages render the same path.
func (s *Server) loadAuthTypeFile() (key.Map, error) {
	file, err := os.Open(s.config.AuthType)
	if err != nil {
		return nil, fmt.Errorf("read authorized keys file '%s': %w", s.config.AuthType, err)
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, maxAuthorizedKeysFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read authorized keys file '%s': %w", s.config.AuthType, err)
	}
	if len(b) > maxAuthorizedKeysFileSize {
		return nil, fmt.Errorf("read authorized keys file '%s': exceeds %d-byte limit", s.config.AuthType, maxAuthorizedKeysFileSize)
	}
	keys, err := key.ParseKeys(b)
	if err != nil {
		return nil, fmt.Errorf("parse authorized keys file '%s': %w", s.config.AuthType, err)
	}
	return keys, nil
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
	// Validate the file before accepting connections. Each authentication attempt
	// reloads it independently so replacements and revocations take effect
	// immediately without sharing mutable callback state.
	keys, err := s.loadAuthTypeFile()
	if err != nil {
		return fmt.Errorf("initialize authorized keys: %w", err)
	}
	var failureLog struct {
		sync.Mutex
		last string
	}
	authorize := func(pubkey ssh.PublicKey) (*ssh.Permissions, error) {
		keys, err := s.loadAuthTypeFile()
		if err != nil {
			message := err.Error()
			failureLog.Lock()
			if failureLog.last != message {
				s.errorf("Failed to reload authorized keys: %s", err)
				failureLog.last = message
			}
			failureLog.Unlock()
			return nil, fmt.Errorf("denied")
		}
		failureLog.Lock()
		failureLog.last = ""
		failureLog.Unlock()
		return nil, s.matchKeys(pubkey, keys)
	}
	sc.PublicKeyCallback = func(conn ssh.ConnMetadata, pubkey ssh.PublicKey) (*ssh.Permissions, error) {
		return authorize(pubkey)
	}
	// PublicKeyCallback results are cached between the unsigned key query and
	// the signed authentication request. Recheck after signature verification
	// so a revocation during that window is enforced for the current handshake.
	sc.VerifiedPublicKeyCallback = func(conn ssh.ConnMetadata, pubkey ssh.PublicKey, permissions *ssh.Permissions, signatureAlgorithm string) (*ssh.Permissions, error) {
		return authorize(pubkey)
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
