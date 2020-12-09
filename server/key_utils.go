package sshd

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

func generateKey(seed string) ([]byte, error) {
	var r io.Reader
	if seed == "" {
		r = rand.Reader
	} else {
		r = newDetermRand([]byte(seed))
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

func fingerprint(k ssh.PublicKey) string {
	bytes := md5.Sum(k.Marshal())
	strbytes := make([]string, len(bytes))
	for i, b := range bytes {
		strbytes[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(strbytes, ":")
}

//========

const determRandIter = 2048

func newDetermRand(seed []byte) io.Reader {
	var out []byte
	//strengthen seed
	var next = seed
	for i := 0; i < determRandIter; i++ {
		next, out = hash(next)
	}
	return &determRand{
		next: next,
		out:  out,
	}
}

type determRand struct {
	next, out []byte
}

func (d *determRand) Read(b []byte) (int, error) {
	l := len(b)
	//HACK: combat https://golang.org/src/crypto/rsa/rsa.go#L257
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
