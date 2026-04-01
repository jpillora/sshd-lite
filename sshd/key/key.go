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
	"errors"
	"fmt"
	"io"
	"math/big"
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
	priv, err := generateRSAKey(r, 2048)
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

var bigOne = big.NewInt(1)

// generateRSAKey generates an RSA key using the provided reader for randomness.
// Unlike rsa.GenerateKey, this uses the reader directly (Go 1.26+ ignores
// custom readers passed to rsa.GenerateKey).
func generateRSAKey(random io.Reader, bits int) (*rsa.PrivateKey, error) {
	e := 65537
	for {
		p, err := randPrime(random, bits/2)
		if err != nil {
			return nil, err
		}
		q, err := randPrime(random, bits/2)
		if err != nil {
			return nil, err
		}
		if p.Cmp(q) == 0 {
			continue
		}
		n := new(big.Int).Mul(p, q)
		if n.BitLen() != bits {
			continue
		}
		pminus1 := new(big.Int).Sub(p, bigOne)
		qminus1 := new(big.Int).Sub(q, bigOne)
		totient := new(big.Int).Mul(pminus1, qminus1)
		d := new(big.Int).ModInverse(big.NewInt(int64(e)), totient)
		if d == nil {
			continue
		}
		priv := &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{N: n, E: e},
			D:         d,
			Primes:    []*big.Int{p, q},
		}
		priv.Precompute()
		if err := priv.Validate(); err != nil {
			return nil, err
		}
		return priv, nil
	}
}

// randPrime generates a random prime of the given bit length using the provided
// reader. This reimplements crypto/rand.Prime to use the reader directly.
func randPrime(r io.Reader, bits int) (*big.Int, error) {
	if bits < 2 {
		return nil, errors.New("prime size must be at least 2-bit")
	}
	b := uint(bits % 8)
	if b == 0 {
		b = 8
	}
	buf := make([]byte, (bits+7)/8)
	p := new(big.Int)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		buf[0] &= uint8(int(1<<b) - 1)
		if b >= 2 {
			buf[0] |= 3 << (b - 2)
		} else {
			buf[0] |= 1
			if len(buf) > 1 {
				buf[1] |= 0x80
			}
		}
		buf[len(buf)-1] |= 1
		p.SetBytes(buf)
		if p.ProbablyPrime(20) {
			return p, nil
		}
	}
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
