package xssh

import (
	"errors"

	"golang.org/x/crypto/ssh"
)

// Request wraps ssh.Request to track whether Reply was called.
type Request struct {
	*ssh.Request
	replied bool
}

// WrapRequest creates a wrapped request that tracks whether Reply was called.
func WrapRequest(req *ssh.Request) *Request {
	return &Request{Request: req}
}

// Reply sends a reply to the request and marks it as replied.
func (r *Request) Reply(ok bool, payload []byte) error {
	if r.replied {
		return errors.New("request already replied to")
	}
	r.replied = true
	return r.Request.Reply(ok, payload)
}

// Replied returns true if Reply has been called.
func (r *Request) Replied() bool {
	return r.replied
}
