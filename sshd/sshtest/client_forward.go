package sshtest

import (
	"fmt"
	"io"
	"net"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

func (c *clientGo) trackForwardListener(listener net.Listener) bool {
	// Keep connection state and listener registration in one critical section.
	// Close takes these locks in the same order before snapshotting, so a
	// listener is either included in that snapshot or rejected after disconnect.
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return false
	}
	c.forwardMu.Lock()
	c.forwardListeners[listener] = struct{}{}
	c.forwardMu.Unlock()
	return true
}

func (c *clientGo) untrackForwardListener(listener net.Listener) {
	c.forwardMu.Lock()
	delete(c.forwardListeners, listener)
	c.forwardMu.Unlock()
}

// LocalForward creates a local port forward.
func (c *clientGo) LocalForward(localAddr, remoteAddr string) (net.Listener, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", localAddr, err)
	}
	if !c.trackForwardListener(listener) {
		_ = listener.Close()
		return nil, fmt.Errorf("connection closed while creating local forward")
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "local", "local", localAddr, "remote", remoteAddr)

	go func() {
		defer c.untrackForwardListener(listener)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(conn net.Conn) {
				defer conn.Close()

				remote, err := client.Dial("tcp", remoteAddr)
				if err != nil {
					return
				}
				defer remote.Close()

				// Bidirectional copy
				done := make(chan struct{}, 2)
				go func() {
					io.Copy(remote, conn)
					done <- struct{}{}
				}()
				go func() {
					io.Copy(conn, remote)
					done <- struct{}{}
				}()
				<-done
			}(conn)
		}
	}()

	return listener, nil
}

// RemoteForward requests a remote port forward. It returns only setup errors;
// the client retains ownership of the listener until Client.Close.
func (c *clientGo) RemoteForward(remoteAddr, localAddr string) error {
	_, err := c.remoteForwardListener(remoteAddr, localAddr)
	return err
}

func (c *clientGo) remoteForwardListener(remoteAddr, localAddr string) (net.Listener, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to request remote forward: %w", err)
	}
	if !c.trackForwardListener(listener) {
		_ = listener.Close()
		return nil, fmt.Errorf("connection closed while creating remote forward")
	}

	c.events.Emit(scenario.EventForwardRequested, "client", c.config.name, "type", "remote", "remote", remoteAddr, "local", localAddr)

	go func() {
		defer c.untrackForwardListener(listener)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(conn net.Conn) {
				defer conn.Close()

				local, err := net.Dial("tcp", localAddr)
				if err != nil {
					return
				}
				defer local.Close()

				done := make(chan struct{}, 2)
				go func() {
					io.Copy(local, conn)
					done <- struct{}{}
				}()
				go func() {
					io.Copy(conn, local)
					done <- struct{}{}
				}()
				<-done
			}(conn)
		}
	}()

	return listener, nil
}
