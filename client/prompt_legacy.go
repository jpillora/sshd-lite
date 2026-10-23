package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Connect keeps its established platform prompt support. Dial's typed adapter
// admits only the platforms with cancellable terminal ownership.
func legacyInteraction() Interaction {
	return Interaction{Passphrase: func(ctx context.Context, name string) ([]byte, error) {
		return legacyTerminalRead(ctx, "Private key passphrase: ", true)
	}}
}

func promptPassword(prompt string) (string, error) {
	b, e := legacyTerminalRead(context.Background(), prompt, true)
	defer clear(b)
	return string(b), e
}

func confirmHostKey(hostname string, key ssh.PublicKey, mismatch bool) (bool, error) {
	return confirmHostKeyContext(context.Background(), hostname, key, mismatch)
}
func confirmHostKeyContext(ctx context.Context, hostname string, key ssh.PublicKey, mismatch bool) (bool, error) {
	var prompt bytes.Buffer
	writeHostKeyPrompt(&prompt, hostname, key, mismatch)
	b, e := legacyTerminalRead(ctx, prompt.String(), false)
	if e != nil {
		return false, e
	}
	defer clear(b)
	return readConfirmation(bytes.NewReader(b))
}

func writeHostKeyPrompt(w io.Writer, hostname string, key ssh.PublicKey, mismatch bool) {
	fingerprint := ssh.FingerprintSHA256(key)
	if mismatch {
		fmt.Fprintln(w, "\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@")
		fmt.Fprintln(w, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!")
		fmt.Fprintln(w, "This could mean that someone is intercepting your connection.")
		fmt.Fprintf(w, "New key for %s is %s %s.\n", hostname, key.Type(), fingerprint)
		fmt.Fprint(w, "Replace the saved key? (y/N) ")
	} else {
		fmt.Fprintf(w, "The authenticity of host %s cannot be established.\n", hostname)
		fmt.Fprintf(w, "%s key fingerprint is %s.\n", key.Type(), fingerprint)
		fmt.Fprint(w, "Add this host to known_hosts? (y/N) ")
	}
}

func readConfirmation(r io.Reader) (bool, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
