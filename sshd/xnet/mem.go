package xnet

import (
	"context"
	"net"

	"google.golang.org/grpc/test/bufconn"
)

// ListenerDialer combines a net.Listener with a Dial method.
type ListenerDialer interface {
	net.Listener
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// mem implements ListenerDialer using an in-memory buffer.
type mem struct {
	*bufconn.Listener
}

// NewMem creates an in-memory ListenerDialer using bufconn.
// The buffer size is 32KB per connection (matches SSH max packet size).
func NewMem() ListenerDialer {
	return &mem{
		Listener: bufconn.Listen(32 * 1024),
	}
}

// Dial connects to the in-memory listener.
// The network and addr parameters are ignored.
func (m *mem) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return m.Listener.DialContext(ctx)
}
