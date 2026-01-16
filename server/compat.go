// Package sshd provides type aliases for the sshd package.
//
// Deprecated: Use the ./sshd package instead.
package sshd

import (
	"github.com/jpillora/sshd-lite/sshd"
	"github.com/jpillora/sshd-lite/xssh"
)

// Deprecated: Use github.com/jpillora/sshd-lite/xssh.FdHolder instead.
type FdHolder = xssh.FdHolder

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.Config instead.
type Config = sshd.Config

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.Server instead.
type Server = sshd.Server

// Deprecated: Use github.com/jpillora/sshd-lite/xssh.TCPForwardingHandler instead.
type TCPForwardingHandler = xssh.TCPForwardingHandler

// Deprecated: Use github.com/jpillora/sshd-lite/xssh.SetWinsize instead.
func SetWinsize(t FdHolder, w, h uint32) {
	xssh.SetWinsize(t, w, h)
}

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.NewConfig instead.
func NewConfig(keyFile string, keySeed string) *Config {
	return &sshd.Config{
		KeyFile: keyFile,
		KeySeed: keySeed,
	}
}

// Deprecated: Use github.com/jpillora/sshd-lite/sshd.NewServer instead.
func NewServer(c *Config) (*Server, error) {
	return sshd.NewServer(*c)
}
