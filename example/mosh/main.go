// This program embeds both peers. No sshd-lite, ssh, or mosh executable is run.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/jpillora/sshd-lite/mosh"
	"github.com/jpillora/sshd-lite/sshd"
	"golang.org/x/crypto/ssh"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func key() ([]byte, ssh.Signer, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return nil, nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(block), signer, nil
}
func run() error {
	hostBytes, hostKey, err := key()
	if err != nil {
		return err
	}
	_, userKey, err := key()
	if err != nil {
		return err
	}
	server, err := sshd.NewServer(sshd.Config{Attach: mosh.Attach, KeyBytes: hostBytes, AuthKeys: []ssh.PublicKey{userKey.PublicKey()}, LogQuiet: true})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- server.StartWithContext(ctx, listener) }()
	defer func() { cancel(); <-stopped }()
	var screen bytes.Buffer
	session, err := mosh.Dial(ctx, listener.Addr().String(), &ssh.ClientConfig{
		User: "demo", Auth: []ssh.AuthMethod{ssh.PublicKeys(userKey)}, HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
	}, mosh.ClientConfig{Columns: 80, Rows: 24, Output: &screen})
	if err != nil {
		return err
	}
	defer session.Close()
	if _, err := io.WriteString(session, "echo Hello from Go\nexit 7\n"); err != nil {
		return err
	}
	code, err := session.Wait()
	if err != nil {
		return err
	}
	// Output contains ANSI screen updates. A GUI could render these as they
	// arrive; a bytes.Buffer may be inspected once Wait/Done has completed.
	fmt.Printf("Remote exit status: %d; received %d bytes of terminal updates\n", code, screen.Len())
	if code != 7 {
		return fmt.Errorf("unexpected remote exit status: %d", code)
	}
	return nil
}
