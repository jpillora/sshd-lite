package sshd_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/xhttp"
	"github.com/jpillora/sshd-lite/sshd/xnet"
	"golang.org/x/crypto/ssh"
)

var tcpForwardingLocal = testCase{
	name: "tcp-forwarding-local",
	options: []sshtest.ServerOption{
		sshtest.ServerWithNoAuth(),
		sshtest.ServerWithTCPForwarding(true),
	},
	client: func(addr string) (retErr error) {
		// 1. Start a test HTTP server
		httpServer, err := xhttp.NewTestServer("foo")
		if err != nil {
			return err
		}
		defer httpServer.Close()

		// 2. Connect to SSH server
		c, err := sshtest.CreateSSHClient(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to ssh: %w", err)
		}
		defer func() {
			if err := c.Close(); retErr == nil && err != nil {
				retErr = fmt.Errorf("failed to close SSH client: %w", err)
			}
		}()

		// 3. Setup local port forwarding (reverse tunnel)
		// This is actually "remote" forwarding from the SSH server's perspective:
		// - We ask the SSH server to listen on a port
		// - When connections come in, the server forwards them back to us
		// - We then forward them to our local HTTP server
		localPort, err := xnet.GetRandomPort()
		if err != nil {
			return fmt.Errorf("failed to get random port: %w", err)
		}

		// Request the SSH server to listen on this port and forward connections back to us
		localConn, err := c.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", localPort))
		if err != nil {
			return fmt.Errorf("failed to setup port forwarding: %w", err)
		}
		defer func() {
			if err := localConn.Close(); retErr == nil && err != nil {
				retErr = fmt.Errorf("failed to cancel port forwarding: %w", err)
			}
		}()

		// Handle forwarding connections in background
		go func() {
			for {
				conn, err := localConn.Accept()
				if err != nil {
					return
				}
				go func(conn net.Conn) {
					httpConn, err := net.Dial("tcp", httpServer.Addr)
					if err != nil {
						conn.Close()
						return
					}
					xnet.ForwardConnections(conn, httpConn)
				}(conn)
			}
		}()

		// 4. Test HTTP request through port forward
		return xhttp.TestGet(fmt.Sprintf("http://127.0.0.1:%s/", localPort), "foo")
	},
}

var tcpForwardingRemote = testCase{
	name: "tcp-forwarding-remote",
	options: []sshtest.ServerOption{
		sshtest.ServerWithNoAuth(),
		sshtest.ServerWithTCPForwarding(true),
	},
	client: func(addr string) (retErr error) {
		// 1. Connect to SSH server
		c, err := sshtest.CreateSSHClient(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to ssh: %w", err)
		}
		defer func() {
			if err := c.Close(); retErr == nil && err != nil {
				retErr = fmt.Errorf("failed to close SSH client: %w", err)
			}
		}()

		// 2. Request remote port forwarding (reverse tunnel)
		// This asks the SSH server to listen on a port and forward incoming connections back to us
		// c.Listen() creates a "reverse tunnel" - the server listens, we handle the connections
		remoteListener, err := c.Listen("tcp", "127.0.0.1:0") // 0 = random port
		if err != nil {
			return fmt.Errorf("failed to setup remote port forwarding: %w", err)
		}
		defer func() {
			if err := remoteListener.Close(); retErr == nil && err != nil {
				retErr = fmt.Errorf("failed to cancel remote port forwarding: %w", err)
			}
		}()

		// Get the actual remote port that was allocated by the SSH server
		remoteAddr := remoteListener.Addr().String()

		// 3. Start our local HTTP server that will receive the forwarded connections
		httpServer, err := xhttp.NewTestServer("bar")
		if err != nil {
			return err
		}
		defer httpServer.Close()

		// 4. Handle incoming connections from the remote port forwarding
		// When someone connects to the SSH server's bound port, we'll get the connection here
		connectionReceived := make(chan error, 1)
		go func() {
			conn, err := remoteListener.Accept()
			if err != nil {
				connectionReceived <- fmt.Errorf("failed to accept remote connection: %w", err)
				return
			}

			// Forward this connection to our local HTTP server
			httpConn, err := net.Dial("tcp", httpServer.Addr)
			if err != nil {
				conn.Close()
				connectionReceived <- fmt.Errorf("failed to connect to local http server: %w", err)
				return
			}

			connectionReceived <- nil
			xnet.ForwardConnections(conn, httpConn)
		}()

		// 5. Make an HTTP request to the remote port
		// This simulates an external client connecting to the SSH server's bound port
		// The SSH server will forward this connection back to us through the tunnel
		httpClient := &http.Client{Timeout: 3 * time.Second}
		resp, err := httpClient.Get(fmt.Sprintf("http://%s/", remoteAddr))
		if err != nil {
			return fmt.Errorf("failed to make http request to remote port: %w", err)
		}
		defer resp.Body.Close()

		// Wait for the connection to be established
		select {
		case err := <-connectionReceived:
			if err != nil {
				return err
			}
		case <-time.After(2 * time.Second):
			return fmt.Errorf("timeout waiting for remote connection")
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		if string(body) != "bar" {
			return fmt.Errorf("unexpected response: got %q, want %q", string(body), "bar")
		}

		return nil
	},
}

func TestTCPForwardingDynamicPortCancellationAndImmediateRebind(t *testing.T) {
	server := startTCPForwardingServer(t)
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect SSH client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close SSH client: %v", err)
		}
	})

	forward, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("request dynamic remote forwarding: %v", err)
	}
	addr := forward.Addr().String()
	if err := forward.Close(); err != nil {
		t.Fatalf("cancel dynamic remote forwarding: %v", err)
	}

	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("rebind %s immediately after successful cancellation: %v", addr, err)
	}
	if err := rebound.Close(); err != nil {
		t.Fatalf("close rebound listener: %v", err)
	}
}

func TestTCPForwardingSuccessReplyPayload(t *testing.T) {
	server := startTCPForwardingServer(t)
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect SSH client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close SSH client: %v", err)
		}
	})

	host := "127.0.0.1"
	dynamicRequest := ssh.Marshal(&struct {
		Host string
		Port uint32
	}{Host: host, Port: 0})
	ok, reply, err := client.SendRequest("tcpip-forward", true, dynamicRequest)
	if err != nil {
		t.Fatalf("request dynamic forwarding: %v", err)
	}
	if !ok {
		t.Fatal("dynamic forwarding request was rejected")
	}
	var allocated struct{ Port uint32 }
	if err := ssh.Unmarshal(reply, &allocated); err != nil {
		t.Fatalf("decode dynamic forwarding reply %x: %v", reply, err)
	}
	if allocated.Port == 0 {
		t.Fatal("dynamic forwarding reply contained port zero")
	}

	cancelRequest := ssh.Marshal(&struct {
		Host string
		Port uint32
	}{Host: host, Port: allocated.Port})
	ok, _, err = client.SendRequest("cancel-tcpip-forward", true, cancelRequest)
	if err != nil {
		t.Fatalf("cancel dynamic forwarding: %v", err)
	}
	if !ok {
		t.Fatal("dynamic forwarding cancellation was rejected")
	}

	fixedRequest := ssh.Marshal(&struct {
		Host string
		Port uint32
	}{Host: host, Port: allocated.Port})
	ok, reply, err = client.SendRequest("tcpip-forward", true, fixedRequest)
	if err != nil {
		t.Fatalf("request fixed-port forwarding: %v", err)
	}
	if !ok {
		t.Fatal("fixed-port forwarding request was rejected")
	}
	if len(reply) != 0 {
		t.Fatalf("fixed-port forwarding reply = %x, want empty payload", reply)
	}

	ok, _, err = client.SendRequest("cancel-tcpip-forward", true, fixedRequest)
	if err != nil {
		t.Fatalf("cancel fixed-port forwarding: %v", err)
	}
	if !ok {
		t.Fatal("fixed-port forwarding cancellation was rejected")
	}
}

func TestTCPForwardingListenerClosesOnClientDisconnect(t *testing.T) {
	server := startTCPForwardingServer(t)
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect SSH client: %v", err)
	}

	forward, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		t.Fatalf("request dynamic remote forwarding: %v", err)
	}
	addr := forward.Addr().String()

	// Deliberately close only the SSH transport. No cancel-tcpip-forward is sent.
	if err := client.Close(); err != nil {
		t.Fatalf("close SSH client: %v", err)
	}
	rebound := waitForTCPRebind(t, addr)
	if err := rebound.Close(); err != nil {
		t.Fatalf("close rebound listener: %v", err)
	}
}

func TestTCPForwardingRejectsDuplicateRequest(t *testing.T) {
	server := startTCPForwardingServer(t)
	client, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect SSH client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close SSH client: %v", err)
		}
	})

	forward, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("request dynamic remote forwarding: %v", err)
	}
	host, portString, err := net.SplitHostPort(forward.Addr().String())
	if err != nil {
		t.Fatalf("split forwarding address: %v", err)
	}
	var port uint32
	if _, err := fmt.Sscanf(portString, "%d", &port); err != nil {
		t.Fatalf("parse forwarding port: %v", err)
	}
	payload := ssh.Marshal(&struct {
		Host string
		Port uint32
	}{Host: host, Port: port})
	ok, _, err := client.SendRequest("tcpip-forward", true, payload)
	if err != nil {
		t.Fatalf("send duplicate forwarding request: %v", err)
	}
	if ok {
		t.Fatal("duplicate forwarding request was accepted")
	}
	if err := forward.Close(); err != nil {
		t.Fatalf("cancel original forwarding after duplicate request: %v", err)
	}
}

func TestTCPForwardingCleanupIsPerConnection(t *testing.T) {
	server := startTCPForwardingServer(t)
	client1, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		t.Fatalf("connect first SSH client: %v", err)
	}
	client2, err := sshtest.CreateSSHClient(server.Addr())
	if err != nil {
		_ = client1.Close()
		t.Fatalf("connect second SSH client: %v", err)
	}
	t.Cleanup(func() {
		if err := client2.Close(); err != nil {
			t.Errorf("close second SSH client: %v", err)
		}
	})

	forward1, err := client1.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client1.Close()
		t.Fatalf("request first remote forwarding: %v", err)
	}
	forward2, err := client2.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client1.Close()
		t.Fatalf("request second remote forwarding: %v", err)
	}

	if err := client1.Close(); err != nil {
		t.Fatalf("close first SSH client: %v", err)
	}
	rebound1 := waitForTCPRebind(t, forward1.Addr().String())

	probe, err := net.Listen("tcp", forward2.Addr().String())
	if err == nil {
		_ = probe.Close()
		t.Fatal("closing the first SSH connection also closed the second connection's listener")
	}
	if err := forward2.Close(); err != nil {
		t.Fatalf("cancel second connection's forwarding: %v", err)
	}
	rebound2, err := net.Listen("tcp", forward2.Addr().String())
	if err != nil {
		t.Fatalf("rebind second forwarding after cancellation: %v", err)
	}
	if err := rebound2.Close(); err != nil {
		t.Fatalf("close second rebound listener: %v", err)
	}
	if err := rebound1.Close(); err != nil {
		t.Fatalf("close first rebound listener: %v", err)
	}
}

func startTCPForwardingServer(t *testing.T) sshtest.Server {
	t.Helper()
	server, err := sshtest.NewServer(
		sshtest.ServerWithNoAuth(),
		sshtest.ServerWithTCPForwarding(true),
	)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Errorf("stop server: %v", err)
		}
	})
	return server
}

func waitForTCPRebind(t *testing.T, addr string) net.Listener {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener %s was not released after SSH disconnect: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
