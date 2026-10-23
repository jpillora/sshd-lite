package xssh

import (
	"encoding/binary"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

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

		err := sess.dispatchRequest(req, func() error {
			if req.Type == "subsystem" {
				if !c.handleSubsystemRequest(sess, req) {
					return fmt.Errorf("subsystem request rejected")
				}
				return nil
			}
			handler, ok := c.sessionRequestHandlers[req.Type]
			if !ok {
				return fmt.Errorf("unknown session request: %s", req.Type)
			}
			return handler(sess, req)
		})
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
