// Package xhttp retains the legacy HTTP test helpers.
// Deprecated: use net/http/httptest for new callers.
package xhttp

import "github.com/jpillora/sshd-lite/internal/testutil"

type TestServer = testutil.TestServer

func NewTestServer(message string) (*TestServer, error) { return testutil.NewTestServer(message) }
func TestGet(url, expected string) error                { return testutil.TestGet(url, expected) }
