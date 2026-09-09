// Package sshtest provides an AI-friendly test harness for SSH servers and clients.
//
// The harness provides a fluent API for setting up test environments:
//
//	env := sshtest.New(t).
//		WithServer(sshtest.ServerWithSFTP(true)).
//		WithClient("alice", sshtest.ClientWithKeySeed("alice")).
//		Start()
//	defer env.Stop()
//
//	result, err := env.Client("alice").Exec("echo hello")
package sshtest
