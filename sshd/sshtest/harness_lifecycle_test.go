package sshtest

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

func TestEnvironmentStopClosesSessionsAndForwardsAndIsIdempotent(t *testing.T) {
	env := New(t).
		WithServer().
		WithClient("test", ClientWithKeySeed("test"), ClientWithPTY(80, 24)).
		Start()
	client := env.Client("test")
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := StartShell().Execute(context.Background(), env, "test"); err != nil {
		t.Fatalf("start tracked shell: %v", err)
	}
	if err := LocalForward("127.0.0.1:0", "127.0.0.1:1").Execute(context.Background(), env, "test"); err != nil {
		t.Fatalf("start tracked forward: %v", err)
	}

	env.mu.Lock()
	listeners := env.forwardListeners["test"]
	env.mu.Unlock()
	if len(listeners) != 1 {
		t.Fatalf("tracked listeners = %d, want 1", len(listeners))
	}
	listener, ok := listeners[0].(net.Listener)
	if !ok {
		t.Fatalf("tracked forward has type %T", listeners[0])
	}
	addr := listener.Addr().String()

	env.Stop()
	env.Stop()
	if client.IsConnected() {
		t.Fatal("client remains connected after Stop")
	}
	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("forward listener %s was not released: %v", addr, err)
	}
	_ = rebound.Close()
}

func TestClientCloseConcurrentWithForwardCreation(t *testing.T) {
	env := New(t).
		WithServer(ServerWithTCPForwarding(true)).
		WithClient("test", ClientWithKeySeed("test")).
		Start()
	t.Cleanup(env.Stop)
	client := env.Client("test").(*clientGo)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	initialLocal, err := client.LocalForward("127.0.0.1:0", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("create initial local forward: %v", err)
	}
	initialRemote, err := client.remoteForwardListener("127.0.0.1:0", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("create initial remote forward: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var addrMu sync.Mutex
	localAddrs := []string{initialLocal.Addr().String()}
	remoteAddrs := []string{initialRemote.Addr().String()}
	type registration struct {
		listener net.Listener
		err      error
	}
	localRegistered := make(chan registration, 1)
	remoteRegistered := make(chan registration, 1)
	registrationRelease := make(chan struct{})
	releaseRegistrations := sync.OnceFunc(func() { close(registrationRelease) })
	t.Cleanup(releaseRegistrations)
	wg.Add(2)
	go func() {
		defer wg.Done()
		listener, err := client.LocalForward("127.0.0.1:0", "127.0.0.1:1")
		localRegistered <- registration{listener: listener, err: err}
		<-registrationRelease
	}()
	go func() {
		defer wg.Done()
		listener, err := client.remoteForwardListener("127.0.0.1:0", "127.0.0.1:1")
		remoteRegistered <- registration{listener: listener, err: err}
		<-registrationRelease
	}()
	for kind, result := range map[string]registration{"local": <-localRegistered, "remote": <-remoteRegistered} {
		if result.err != nil || result.listener == nil {
			t.Fatalf("guaranteed %s registration failed during Close race setup: %v", kind, result.err)
		}
		if kind == "local" {
			localAddrs = append(localAddrs, result.listener.Addr().String())
		} else {
			remoteAddrs = append(remoteAddrs, result.listener.Addr().String())
		}
	}

	for i := 0; i < 24; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			listener, err := client.LocalForward("127.0.0.1:0", "127.0.0.1:1")
			if err == nil {
				addrMu.Lock()
				localAddrs = append(localAddrs, listener.Addr().String())
				addrMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			listener, err := client.remoteForwardListener("127.0.0.1:0", "127.0.0.1:1")
			if err == nil {
				addrMu.Lock()
				remoteAddrs = append(remoteAddrs, listener.Addr().String())
				addrMu.Unlock()
			}
		}()
	}
	close(start)
	if err := client.Close(); err != nil {
		t.Fatalf("concurrent close: %v", err)
	}
	releaseRegistrations()
	wg.Wait()
	if len(localAddrs) < 2 || len(remoteAddrs) < 2 {
		t.Fatalf("successful dynamic forwards including raced registrations: local=%d remote=%d", len(localAddrs), len(remoteAddrs))
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		client.forwardMu.Lock()
		remaining := len(client.forwardListeners)
		client.forwardMu.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d forwarding listeners remain tracked after Close", remaining)
		}
		time.Sleep(time.Millisecond)
	}
	for kind, addrs := range map[string][]string{"local": localAddrs, "remote": remoteAddrs} {
		for _, addr := range addrs {
			listener := waitForListenerRebind(t, addr)
			if err := listener.Close(); err != nil {
				t.Fatalf("close rebound %s listener %s: %v", kind, addr, err)
			}
		}
	}
}

func TestForwardRegistrationBoundaryOverlapsClose(t *testing.T) {
	for _, kind := range []string{"local", "remote"} {
		t.Run(kind, func(t *testing.T) {
			env := New(t).
				WithServer(ServerWithTCPForwarding(true)).
				WithClient("test", ClientWithKeySeed("test")).
				Start()
			t.Cleanup(env.Stop)
			client := env.Client("test").(*clientGo)
			if err := client.Connect(); err != nil {
				t.Fatalf("connect: %v", err)
			}

			// Hold forwardMu so the real registration worker acquires client.mu
			// and blocks exactly at the ownership-publication boundary. Close then
			// queues on client.mu. Releasing forwardMu forces registration to win;
			// Close must subsequently snapshot and close that new listener.
			client.forwardMu.Lock()
			releaseForwardMu := sync.OnceFunc(client.forwardMu.Unlock)
			t.Cleanup(releaseForwardMu)
			type result struct {
				listener net.Listener
				err      error
			}
			registered := make(chan result, 1)
			go func() {
				if kind == "local" {
					listener, err := client.LocalForward("127.0.0.1:0", "127.0.0.1:1")
					registered <- result{listener: listener, err: err}
					return
				}
				listener, err := client.remoteForwardListener("127.0.0.1:0", "127.0.0.1:1")
				registered <- result{listener: listener, err: err}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if !client.mu.TryLock() {
					break
				}
				client.mu.Unlock()
				if time.Now().After(deadline) {
					t.Fatal("registration worker did not reach forwardMu boundary")
				}
				time.Sleep(time.Millisecond)
			}
			closeStarted := make(chan struct{})
			closeResult := make(chan error, 1)
			go func() {
				close(closeStarted)
				closeResult <- client.Close()
			}()
			<-closeStarted
			// Give the Close goroutine an execution turn while registration owns
			// client.mu. This remains deterministic with GOMAXPROCS=1.
			time.Sleep(time.Millisecond)
			releaseForwardMu()

			registration := <-registered
			if registration.err != nil || registration.listener == nil {
				t.Fatalf("%s registration at Close boundary failed: %v", kind, registration.err)
			}
			addr := registration.listener.Addr().String()
			if err := <-closeResult; err != nil {
				t.Fatalf("Close after %s boundary registration: %v", kind, err)
			}
			client.forwardMu.Lock()
			remaining := len(client.forwardListeners)
			client.forwardMu.Unlock()
			if remaining != 0 {
				t.Fatalf("%d listeners remain after %s boundary registration", remaining, kind)
			}
			rebound := waitForListenerRebind(t, addr)
			_ = rebound.Close()
		})
	}
}

func TestEnvironmentStartStopPublicationAndConcurrentStopJoin(t *testing.T) {
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	env := New(t).
		WithServer(func(*serverConfig) {
			close(startEntered)
			<-releaseStart
		}).
		WithClient("test", ClientWithKeySeed("test"))
	startDone := make(chan struct{})
	go func() {
		env.Start()
		close(startDone)
	}()
	<-startEntered
	stopDone := make(chan struct{})
	go func() {
		env.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned while Start still held unpublished state")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseStart)
	select {
	case <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not complete")
	}
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("racing Stop did not complete")
	}
	server := env.Server()
	if server == nil {
		t.Fatal("fully initialized server was not published before Stop")
	}
	rebound := waitForListenerRebind(t, server.Addr())
	_ = rebound.Close()

	second := New(t).WithServer().WithClient("test", ClientWithKeySeed("test")).Start()
	blocker := newBlockingCloser()
	second.mu.Lock()
	second.forwardListeners["test"] = append(second.forwardListeners["test"], blocker)
	secondServerAddr := second.server.Addr()
	second.mu.Unlock()
	firstStop := make(chan struct{})
	go func() { second.Stop(); close(firstStop) }()
	<-blocker.entered
	joinedStop := make(chan struct{})
	joinedStarted := make(chan struct{})
	go func() { close(joinedStarted); second.Stop(); close(joinedStop) }()
	<-joinedStarted
	select {
	case <-joinedStop:
		t.Fatal("concurrent Stop returned before active teardown completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(blocker.release)
	for name, done := range map[string]<-chan struct{}{"first": firstStop, "joined": joinedStop} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s Stop did not complete", name)
		}
	}
	rebound = waitForListenerRebind(t, secondServerAddr)
	_ = rebound.Close()
}

func TestClientReconnectWaitsForCloseTeardownAndEvent(t *testing.T) {
	env := New(t).WithServer().WithClient("test", ClientWithKeySeed("test")).Start()
	t.Cleanup(env.Stop)
	client := env.Client("test").(*clientGo)
	if err := client.Connect(); err != nil {
		t.Fatalf("initial connect: %v", err)
	}
	blocker := newBlockingListener()
	client.forwardMu.Lock()
	client.forwardListeners[blocker] = struct{}{}
	client.forwardMu.Unlock()
	closeResult := make(chan error, 1)
	go func() { closeResult <- client.Close() }()
	<-blocker.entered
	connectResult := make(chan error, 1)
	connectStarted := make(chan struct{})
	go func() { close(connectStarted); connectResult <- client.Connect() }()
	<-connectStarted
	select {
	case err := <-connectResult:
		t.Fatalf("reconnect overlapped Close teardown: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(blocker.release)
	if err := <-closeResult; err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := <-connectResult; err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	events := env.Events().All()
	var sequence []string
	for _, event := range events {
		if event.ID == scenario.EventConnected || event.ID == scenario.EventDisconnected {
			sequence = append(sequence, event.ID)
		}
	}
	want := []string{scenario.EventConnected, scenario.EventDisconnected, scenario.EventConnected}
	if len(sequence) != len(want) {
		t.Fatalf("connection event sequence = %v, want %v", sequence, want)
	}
	for i := range want {
		if sequence[i] != want[i] {
			t.Fatalf("connection event sequence = %v, want %v", sequence, want)
		}
	}
}

func TestEnvironmentOutputStateConcurrentWithStop(t *testing.T) {
	env := New(t)
	env.mu.Lock()
	env.started = true
	blocker := newBlockingCloser()
	releaseBlocker := sync.OnceFunc(func() { close(blocker.release) })
	t.Cleanup(releaseBlocker)
	env.forwardListeners["test"] = append(env.forwardListeners["test"], blocker)
	env.lastExecResult = &ExecResult{Stdout: "initial", ExitCode: 7}
	env.mu.Unlock()
	stopDone := make(chan struct{})
	go func() { env.Stop(); close(stopDone) }()
	<-blocker.entered

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for iteration := 0; iteration < 200; iteration++ {
				env.execResult()
				env.outputState("test")
				env.sessionByName("test")
				if env.storeExecResult(&ExecResult{Stdout: "late", ExitCode: index}) {
					t.Errorf("exec result published while environment was stopping")
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if err := ExpectStdout("initial").Check(context.Background(), env, "test"); err == nil {
		t.Fatal("expectation observed stale exec result after Stop snapshot")
	}
	releaseBlocker()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not complete after output-state regression")
	}
	if _, ok := env.execResult(); ok {
		t.Fatal("exec result remained published after Stop")
	}
}

type blockingCloser struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingCloser() *blockingCloser {
	return &blockingCloser{entered: make(chan struct{}), release: make(chan struct{})}
}

func (c *blockingCloser) Close() error {
	c.once.Do(func() { close(c.entered); <-c.release })
	return nil
}

type blockingListener struct {
	*blockingCloser
}

func newBlockingListener() *blockingListener {
	return &blockingListener{blockingCloser: newBlockingCloser()}
}

func (l *blockingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *blockingListener) Addr() net.Addr            { return testTCPAddr("blocking") }

type testTCPAddr string

func (a testTCPAddr) Network() string { return "test" }
func (a testTCPAddr) String() string  { return string(a) }

func waitForListenerRebind(t *testing.T, addr string) net.Listener {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener %s did not become rebindable: %v", addr, err)
		}
		time.Sleep(time.Millisecond)
	}
}
