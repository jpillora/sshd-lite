// Package sshd provides type aliases for the sshd package.
//
// Deprecated: Use the ./sshd package instead.
package sshd

import (
	"github.com/creack/pty"
	"github.com/jpillora/sshd-lite/sshd"
)

// Deprecated: Use github.com/creack/pty.FdHolder instead.
type FdHolder = pty.FdHolder

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.Config instead.
type Config = sshd.Config

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.Server instead.
type Server = sshd.Server

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.TCPForwardingHandler instead.
type TCPForwardingHandler = sshd.TCPForwardingHandler

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.SetWinsize instead.
func SetWinsize(t FdHolder, w, h uint32) {
	sshd.SetWinsize(t, w, h)
}

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.NewConfig instead.
func NewConfig(keyFile string, keySeed string) *Config {
	return sshd.NewConfig(keyFile, keySeed)
}

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.NewServer instead.
func NewServer(c *Config) (*Server, error) {
	return sshd.NewServer(c)
}
