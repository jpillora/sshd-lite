// Package xnet retains the legacy network test helpers.
// Deprecated: use caller-owned net.Listeners and standard testing helpers.
package xnet

import (
	"net"

	"github.com/jpillora/sshd-lite/internal/testutil"
)

func GetRandomListener() (net.Listener, string, error) { return testutil.GetRandomListener() }
func FindFreePort() (int, error)                       { return testutil.FindFreePort() }
func MustFindFreePort() int                            { return testutil.MustFindFreePort() }
func GetRandomPort() (string, error)                   { return testutil.GetRandomPort() }
func ForwardConnections(a, b net.Conn)                 { testutil.ForwardConnections(a, b) }
