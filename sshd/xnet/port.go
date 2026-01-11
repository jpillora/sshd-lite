package xnet

import (
	"fmt"
	"io"
	"net"
)

// GetRandomListener creates a listener on a random port and returns it along with the address.
func GetRandomListener() (net.Listener, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	addr := listener.Addr().(*net.TCPAddr)
	return listener, addr.String(), nil
}

// FindFreePort returns an available TCP port.
// It works by binding to port 0, which causes the OS to assign an available port.
func FindFreePort() (int, error) {
	listener, addr, err := GetRandomListener()
	if err != nil {
		return 0, fmt.Errorf("failed to find free port: %w", err)
	}
	listener.Close()

	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return 0, err
	}
	return tcpAddr.Port, nil
}

// MustFindFreePort returns an available port or panics.
// Useful for test setup where errors would be fatal anyway.
func MustFindFreePort() int {
	port, err := FindFreePort()
	if err != nil {
		panic(err)
	}
	return port
}

// GetRandomPort returns a random available port number as a string.
func GetRandomPort() (string, error) {
	listener, addr, err := GetRandomListener()
	if err != nil {
		return "", err
	}
	listener.Close()

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}

	return port, nil
}

// ForwardConnections handles bidirectional forwarding between two connections.
func ForwardConnections(conn1, conn2 net.Conn) {
	defer conn1.Close()
	defer conn2.Close()

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(conn1, conn2) //nolint:errcheck
		done <- struct{}{}
	}()

	go func() {
		io.Copy(conn2, conn1) //nolint:errcheck
		done <- struct{}{}
	}()

	<-done // Wait for at least one direction to complete
}
