package mosh

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	protocol "github.com/jpillora/sshd-lite/internal/mosh"
	"golang.org/x/crypto/ssh"
)

// boundedOutput captures bootstrap messages without letting banners exhaust
// memory. Stdout/stderr can be written concurrently by the SSH package.
type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	tooLarge bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(b.data)+n > 64*1024 {
		b.tooLarge = true
		p = p[:max(0, 64*1024-len(b.data))]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func bootstrapMosh(ctx context.Context, conn *ssh.Client, req protocol.Request, server string, commandArgs []string) (protocol.Credentials, error) {
	var credentials protocol.Credentials
	// Cover channel creation too: an SSH peer can withhold its open response.
	timer := time.AfterFunc(10*time.Second, func() { conn.Close() })
	defer timer.Stop()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	session, err := conn.NewSession()
	if err != nil {
		return credentials, err
	}
	defer session.Close()
	terminal := req.Term
	if terminal == "" {
		terminal = "xterm-256color"
	}
	if err := session.RequestPty(terminal, int(req.Rows), int(req.Cols), ssh.TerminalModes{}); err != nil {
		return credentials, err
	}
	var output boundedOutput
	session.Stdout = &output
	session.Stderr = &output
	// Debian accepts locale defaults via -l when its SSH environment isn't UTF-8.
	// Quote every supplied word; there is no interpolation of shell fragments.
	if server == "" {
		server = "mosh-server"
	}
	command := "MOSH_SERVER_NETWORK_TMOUT=300 " + shellQuote(server) + " new -s -c 256 -l LANG=C.UTF-8"
	if len(commandArgs) > 0 {
		command += " --"
		for _, arg := range commandArgs {
			command += " " + shellQuote(arg)
		}
	}
	err = session.Run(command)
	if output.tooLarge {
		return credentials, fmt.Errorf("mosh bootstrap output exceeds 64 KiB")
	}
	if err != nil {
		return credentials, fmt.Errorf("mosh-server startup failed (enable --mosh on sshd-lite, or install mosh-server): %w: %s", err, strings.TrimSpace(string(output.data)))
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output.data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MOSH CONNECT ") {
			continue
		}
		if credentials.Key != "" {
			return credentials, fmt.Errorf("multiple MOSH CONNECT lines")
		}
		fields := strings.Fields(line)
		if len(fields) != 4 || len(fields[3]) != 22 {
			return credentials, fmt.Errorf("invalid MOSH CONNECT line")
		}
		rawKey, keyErr := base64.RawStdEncoding.DecodeString(fields[3])
		if keyErr != nil || len(rawKey) != 16 {
			return credentials, fmt.Errorf("invalid Mosh session key")
		}
		port, err := strconv.Atoi(fields[2])
		if err != nil || port < 1 || port > 65535 {
			return credentials, fmt.Errorf("invalid Mosh UDP port")
		}
		credentials = protocol.Credentials{Port: port, Key: fields[3]}
	}
	if err := scanner.Err(); err != nil {
		return credentials, err
	}
	if credentials.Key == "" {
		return credentials, fmt.Errorf("mosh-server did not return MOSH CONNECT")
	}
	return credentials, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
