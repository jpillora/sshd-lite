package xssh

import (
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Conn wraps an ssh.Conn and handles global requests, channels, and sessions.
// It can be used for both server-side and client-side connections, enabling
// symmetric SSH where either party can offer services to the other.
type Conn interface {
	ssh.Conn

	// Serve starts handling global requests and channels. It blocks until the
	// connection and its owned handler work finish. An overlapping call returns
	// immediately; a later sequential call is allowed.
	Serve()

	// HandleSessionChannel handles the "session" channel type.
	// This is the default handler for "session" channels.
	HandleSessionChannel(newChannel ssh.NewChannel) error

	// Config returns the connection configuration.
	Config() *Config

	// Logging helpers
	debugf(f string, args ...interface{})
	errorf(f string, args ...interface{})
}

type xconn struct {
	inner                ssh.Conn
	config               *Config
	channels             <-chan ssh.NewChannel
	requests             <-chan *ssh.Request
	tcpForwardingHandler *TCPForwardingHandler
	// Handler maps (initialized from config, can be modified)
	globalRequestHandlers  map[string]GlobalRequestHandler
	channelHandlers        map[string]ChannelHandler
	sessionRequestHandlers map[string]SessionRequestHandler
	subsystemHandlers      map[string]SubsystemHandler

	routineMu         sync.Mutex
	serveActive       bool
	acceptingRoutines bool
	routines          sync.WaitGroup
}

// NewConn creates a new xssh.Conn from an established SSH connection.
// The channels and requests parameters are the channels returned by
// ssh.NewServerConn or ssh.NewClientConn.
//
// NewConn panics when config contains a custom handler that conflicts with an
// enabled built-in handler. Use NewConnChecked when configuration can be
// invalid and the caller needs to handle that error.
func NewConn(sshConn ssh.Conn, channels <-chan ssh.NewChannel, requests <-chan *ssh.Request, config *Config) Conn {
	conn, err := NewConnChecked(sshConn, channels, requests, config)
	if err != nil {
		panic(fmt.Sprintf("xssh.NewConn: %v; use NewConnChecked to handle invalid configuration", err))
	}
	return conn
}

// NewConnChecked validates config and creates a new xssh.Conn from an
// established SSH connection. It returns a configuration error instead of
// panicking when a custom handler conflicts with an enabled built-in handler.
func NewConnChecked(sshConn ssh.Conn, channels <-chan ssh.NewChannel, requests <-chan *ssh.Request, config *Config) (Conn, error) {
	if err := ValidateConfig(config); err != nil {
		return nil, err
	}
	if config == nil {
		config = &Config{}
	}
	// Take our own copy: callers share one Config across every connection, and
	// per-connection defaulting below (shell path resolution) writes to it.
	cfg := *config
	xc := &xconn{
		inner:    sshConn,
		config:   &cfg,
		channels: channels,
		requests: requests,
	}
	// Initialize handlers
	xc.globalRequestHandlers = map[string]GlobalRequestHandler{}
	maps.Copy(xc.globalRequestHandlers, config.GlobalRequestHandlers)
	xc.channelHandlers = map[string]ChannelHandler{}
	maps.Copy(xc.channelHandlers, config.ChannelHandlers)
	xc.sessionRequestHandlers = map[string]SessionRequestHandler{}
	maps.Copy(xc.sessionRequestHandlers, config.SessionRequestHandlers)
	xc.subsystemHandlers = map[string]SubsystemHandler{}
	maps.Copy(xc.subsystemHandlers, config.SubsystemHandlers)
	// Register built-in handlers based on config flags
	if config.SFTP {
		xc.subsystemHandlers[SFTPSubsystem] = NewSFTPHandler(SFTPConfig{
			WorkDir: config.WorkingDirectory,
			Logger:  config.Logger,
		})
	}
	if config.LocalForwarding || config.RemoteForwarding {
		tfh := NewTCPForwardingHandler()
		xc.tcpForwardingHandler = tfh
		if config.LocalForwarding {
			xc.channelHandlers[DirectTCPIPChannelType] = tfh.HandleDirectTCPIP
		}
		if config.RemoteForwarding {
			xc.globalRequestHandlers[TCPIPForwardRequestType] = tfh.HandleTCPIPForward
			xc.globalRequestHandlers[CancelTCPIPForwardRequestType] = tfh.HandleCancelTCPIPForward
		}
	}
	// Register built-in session handlers if SessionConfig is set
	xc.registerSessionHandlers()
	return xc, nil
}

// RegisterGlobalRequestHandler registers a handler for a global request type.
func (c *xconn) RegisterGlobalRequestHandler(name string, handler GlobalRequestHandler) {
	c.globalRequestHandlers[name] = handler
}

// RegisterChannelHandler registers a handler for a channel type.
func (c *xconn) RegisterChannelHandler(name string, handler ChannelHandler) {
	c.channelHandlers[name] = handler
}

// RegisterSessionRequestHandler registers a handler for a session request type.
func (c *xconn) RegisterSessionRequestHandler(name string, handler SessionRequestHandler) {
	c.sessionRequestHandlers[name] = handler
}

// RegisterSubsystemHandler registers a handler for a subsystem.
func (c *xconn) RegisterSubsystemHandler(name string, handler SubsystemHandler) {
	c.subsystemHandlers[name] = handler
}

// Config returns the connection configuration.
func (c *xconn) Config() *Config {
	return c.config
}

// Implement ssh.Conn interface by delegating to inner

func (c *xconn) User() string {
	return c.inner.User()
}

func (c *xconn) SessionID() []byte {
	return c.inner.SessionID()
}

func (c *xconn) ClientVersion() []byte {
	return c.inner.ClientVersion()
}

func (c *xconn) ServerVersion() []byte {
	return c.inner.ServerVersion()
}

func (c *xconn) RemoteAddr() net.Addr {
	return c.inner.RemoteAddr()
}

func (c *xconn) LocalAddr() net.Addr {
	return c.inner.LocalAddr()
}

func (c *xconn) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return c.inner.SendRequest(name, wantReply, payload)
}

func (c *xconn) OpenChannel(name string, data []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	return c.inner.OpenChannel(name, data)
}

func (c *xconn) Close() error {
	return c.inner.Close()
}

func (c *xconn) Wait() error {
	return c.inner.Wait()
}

// Serve starts handling global requests and channels. It blocks until the
// connection closes and all connection-owned handler work finishes. An
// overlapping Serve call returns immediately; a later sequential call is
// allowed, though the connection's request and channel streams are normally
// already exhausted. Call Serve in a goroutine if you need to do other work.
func (c *xconn) Serve() {
	c.routineMu.Lock()
	if c.serveActive {
		c.routineMu.Unlock()
		return
	}
	c.serveActive = true
	c.acceptingRoutines = true
	// Keep the count non-zero until every source of new internal routines has
	// stopped. Children may therefore register their own children safely.
	c.routines.Add(1)
	c.routineMu.Unlock()

	// Handle global requests
	c.goRoutine(c.handleGlobalRequests)

	// Handle channels (blocks until channel is closed)
	c.handleChannels()

	// Forwarding transports must close before waiting because their copy and
	// accept routines are part of the connection lifecycle.
	if c.tcpForwardingHandler != nil {
		if err := c.tcpForwardingHandler.closeAll(); err != nil {
			c.errorf("Failed to clean up TCP forwarding: %s", err)
		}
	}

	c.routineMu.Lock()
	c.acceptingRoutines = false
	c.routineMu.Unlock()
	c.routines.Done()
	c.routines.Wait()

	// Keep serveActive set until Wait returns. This prevents another Serve
	// invocation from adding a new anchor to this WaitGroup while it drains.
	c.routineMu.Lock()
	c.serveActive = false
	c.routineMu.Unlock()
}

func (c *xconn) goRoutine(fn func()) bool {
	c.routineMu.Lock()
	if !c.acceptingRoutines {
		c.routineMu.Unlock()
		return false
	}
	c.routines.Add(1)
	c.routineMu.Unlock()
	go func() {
		defer c.routines.Done()
		fn()
	}()
	return true
}

func goConnRoutine(conn Conn, fn func()) bool {
	if c, ok := conn.(*xconn); ok {
		return c.goRoutine(fn)
	}
	go fn()
	return true
}

// handleGlobalRequests dispatches global requests to registered handlers
func (c *xconn) handleGlobalRequests() {
	for sshReq := range c.requests {
		c.debugf("Global request: %s", sshReq.Type)
		handler, ok := c.globalRequestHandlers[sshReq.Type]
		if !ok {
			c.debugf("No handler for global request: %s", sshReq.Type)
			if sshReq.WantReply {
				if err := sshReq.Reply(false, nil); err != nil {
					c.errorf("Failed to reply to global request %q: %s", sshReq.Type, err)
				}
			}
			continue
		}
		req := WrapRequest(sshReq)
		err := handler(c, req)
		if err != nil {
			c.errorf("Global request %q failed: %s", req.Type, err)
		}
		if req.WantReply && !req.Replied() {
			if replyErr := req.Reply(err == nil, nil); replyErr != nil {
				c.errorf("Failed to reply to global request %q: %s", req.Type, replyErr)
			}
		}
	}
}

// handleChannels dispatches incoming channels to registered handlers
func (c *xconn) handleChannels() {
	for newChannel := range c.channels {
		if !c.goRoutine(func() { c.handleChannel(newChannel) }) {
			_ = newChannel.Reject(ssh.ConnectionFailed, "connection is closing")
		}
	}
}

// handleChannel dispatches a single channel to its handler
func (c *xconn) handleChannel(newChannel ssh.NewChannel) {
	channelType := newChannel.ChannelType()
	c.debugf("Channel request '%s'", channelType)

	handler, ok := c.channelHandlers[channelType]
	if !ok {
		c.debugf("Unknown channel type: %s", channelType)
		if err := newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType)); err != nil {
			c.errorf("Failed to reject unknown channel type %q: %s", channelType, err)
		}
		return
	}

	// dispatch to handler with error wrapping
	if err := handler(c, newChannel); err != nil {
		c.errorf("Channel %q failed: %s", channelType, err)
		// handler is responsible for rejecting the channel with appropriate reason
	}
}

// HandleSessionChannel handles the "session" channel type.
// This is the default handler registered for "session" channels.
func (c *xconn) HandleSessionChannel(newChannel ssh.NewChannel) error {
	if d := newChannel.ExtraData(); len(d) > 0 {
		c.debugf("Channel data: '%s' %x", d, d)
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		return fmt.Errorf("could not accept channel: %w", err)
	}
	c.debugf("Channel accepted")

	env, err := sessionEnv(c.config.NoInheritEnv, c.config.NoGlobalEnv, systemEnvFile)
	if err != nil {
		c.errorf("Could not load system environment: %s", err)
	}
	for _, kv := range connectionEnv(c.RemoteAddr(), c.LocalAddr()) {
		env = appendEnv(env, kv)
	}

	// create session and handle requests
	sess := &Session{
		conn:    c,
		done:    make(chan struct{}),
		Channel: channel,
		Env:     env,
		// Resize events use latest-value semantics. A capacity of one lets the
		// request dispatcher replace a stale pending size without blocking.
		Resizes: make(chan []byte, 1),
		Logger:  c.config.Logger,
	}
	sess.startTasks()
	if !c.goRoutine(func() {
		c.handleSessionRequests(sess, requests)
		sess.waitTasks()
	}) {
		sess.closeDone()
		sess.waitTasks()
		_ = channel.Close()
	}
	return nil
}

// handleSessionRequests dispatches session requests to registered handlers
func (c *xconn) handleSessionRequests(sess *Session, requests <-chan *ssh.Request) {
	defer sess.closeDone()
	defer close(sess.Resizes)

	// start keep alive loop
	if ka := c.config.KeepAlive; ka > 0 {
		ticking := make(chan bool, 1)
		interval := time.Duration(ka) * time.Second
		sess.goTask(func() { c.keepAlive(sess.Channel, interval, ticking) })
		defer close(ticking)
	}

	// process requests
	for sshReq := range requests {
		c.debugf("Session request: %s", sshReq.Type)
		req := WrapRequest(sshReq)

		// special case: subsystem requests are dispatched differently
		if sshReq.Type == "subsystem" {
			ok := c.handleSubsystemRequest(sess, req)
			if req.WantReply && !req.Replied() {
				if err := req.Reply(ok, nil); err != nil {
					c.errorf("Failed to reply to subsystem request %q: %s", sshReq.Type, err)
				}
			}
			if !ok {
				sess.Channel.Close()
			}
			continue
		}

		// look up handler
		handler, ok := c.sessionRequestHandlers[sshReq.Type]
		if !ok {
			c.debugf("Unknown session request: %s (reply: %v, data: %x)", sshReq.Type, sshReq.WantReply, sshReq.Payload)
			if req.WantReply && !req.Replied() {
				if err := req.Reply(false, nil); err != nil {
					c.errorf("Failed to reply to unknown session request %q: %s", sshReq.Type, err)
				}
			}
			continue
		}

		// dispatch to handler
		err := handler(sess, req)
		if err != nil {
			c.errorf("Session request %q failed: %s", req.Type, err)
		}
		// auto-reply if handler didn't call Reply
		if req.WantReply && !req.Replied() {
			if replyErr := req.Reply(err == nil, nil); replyErr != nil {
				c.errorf("Failed to reply to session request %q: %s", req.Type, replyErr)
			}
		}
	}
	c.debugf("Closing handler for session requests")
}

// handleSubsystemRequest handles 'subsystem' session requests
func (c *xconn) handleSubsystemRequest(sess *Session, req *Request) bool {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// subsystem name is a string encoded as: [uint32 length][string name]
	if len(req.Payload) < 4 {
		c.debugf("Malformed subsystem request payload")
		return false
	}
	length := binary.BigEndian.Uint32(req.Payload)
	if uint32(len(req.Payload)-4) != length {
		c.debugf("Subsystem name length mismatch in payload")
		return false
	}
	subsystem := string(req.Payload[4:])

	handler, ok := c.subsystemHandlers[subsystem]
	if !ok {
		c.debugf("Unsupported subsystem requested: %q", subsystem)
		return false
	}

	// dispatch to handler with error wrapping
	if err := handler(sess, req); err != nil {
		c.errorf("Subsystem %q failed: %s", subsystem, err)
		return false
	}
	return true
}

// keepAlive sends periodic ping requests to keep the connection alive
func (c *xconn) keepAlive(channel ssh.Channel, interval time.Duration, ticking <-chan bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err := channel.SendRequest("ping", false, nil)
			if err != nil {
				c.debugf("Failed to send keep alive ping: %s", err)
			}
			c.debugf("Sent keep alive ping")
		case <-ticking:
			return
		}
	}
}

// logging helpers
func (c *xconn) debugf(f string, args ...interface{}) {
	if c.config.Logger != nil {
		c.config.Logger.Debug(fmt.Sprintf(f, args...))
	}
}

func (c *xconn) errorf(f string, args ...interface{}) {
	if c.config.Logger != nil {
		c.config.Logger.Error(fmt.Sprintf(f, args...))
	}
}
