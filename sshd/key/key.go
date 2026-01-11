package key

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/crypto/ssh"
)

type Map map[string]string

func (m Map) HasKey(k ssh.PublicKey) bool {
	_, ok := m[string(k.Marshal())]
	return ok
}

func GenerateKey(seed string, ec bool) ([]byte, error) {
	var r io.Reader
	if seed == "" {
		r = rand.Reader
	} else {
		r = NewDetermRand([]byte(seed))
	}
	if ec {
		_, pri, err := ed25519.GenerateKey(r)
		if err != nil {
			return nil, err
		}
		pemBlock, err := ssh.MarshalPrivateKey(pri, "EC PRIVATE KEY")
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(pemBlock), nil
	}
	priv, err := rsa.GenerateKey(r, 2048)
	if err != nil {
		return nil, err
	}
	err = priv.Validate()
	if err != nil {
		return nil, err
	}
	b := x509.MarshalPKCS1PrivateKey(priv)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: b}), nil
}

func GitHubKeys(user string) (Map, error) {
	resp, err := http.Get("https://github.com/" + user + ".keys")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch github user keys: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseKeys(b)
}

func ParseKeys(b []byte) (Map, error) {
	lines := bytes.Split(b, []byte("\n"))
	m := Map{}
	for _, l := range lines {
		if key, cmt, _, _, err := ssh.ParseAuthorizedKey(l); err == nil {
			m[string(key.Marshal())] = cmt
		}
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("no keys found")
	}
	return m, nil
}

func Fingerprint(k ssh.PublicKey) string {
	bytes := sha256.Sum256(k.Marshal())
	b64 := base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes[:])
	return "SHA256:" + b64
}

// SignerFromSeed generates a deterministic SSH signer from a seed string.
// Uses ed25519 for fast key generation. The same seed always produces the same key.
func SignerFromSeed(seed string) (ssh.Signer, error) {
	keyBytes, err := GenerateKey(seed, true) // ed25519
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	return signer, nil
}

// PublicKeyFromSeed generates a deterministic SSH public key from a seed string.
func PublicKeyFromSeed(seed string) (ssh.PublicKey, error) {
	signer, err := SignerFromSeed(seed)
	if err != nil {
		return nil, err
	}
	return signer.PublicKey(), nil
}

// AuthorizedKeyEntry returns an authorized_keys line for a seed.
func AuthorizedKeyEntry(seed string) (string, error) {
	pubKey, err := PublicKeyFromSeed(seed)
	if err != nil {
		return "", err
	}
	return string(ssh.MarshalAuthorizedKey(pubKey)), nil
}

const DetermRandIter = 2048

func NewDetermRand(seed []byte) io.Reader {
	var out []byte
	var next = seed
	for i := 0; i < DetermRandIter; i++ {
		next, out = hash(next)
	}
	return &DetermRand{
		next: next,
		out:  out,
	}
}

type DetermRand struct {
	next, out []byte
}

func (d *DetermRand) Read(b []byte) (int, error) {
	l := len(b)
	if l == 1 {
		return 1, nil
	}
	n := 0
	for n < l {
		next, out := hash(d.next)
		n += copy(b[n:], out)
		d.next = next
	}
	return n, nil
}

func hash(input []byte) (next []byte, output []byte) {
	nextout := sha512.Sum512(input)
	return nextout[:sha512.Size/2], nextout[sha512.Size/2:]
}
