package sshd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSessionEnvironment(t *testing.T) {
	t.Setenv("SSHD_LITE_TEST_PROCESS", "inherited")
	t.Setenv("SSHD_LITE_TEST_OVERRIDE", "process")
	t.Setenv("SSH_CLIENT", "stale")
	t.Setenv("SSH_CONNECTION", "stale")
	t.Setenv("TERM", "stale")
	for _, noInherit := range []bool{false, true} {
		for _, ignoreClient := range []bool{false, true} {
			for _, legacy := range []bool{false, true} {
				t.Run(fmt.Sprintf("noInherit=%v/ignoreClient=%v/legacy=%v", noInherit, ignoreClient, legacy), func(t *testing.T) {
					captured := make(chan []string, 1)
					noClientEnv, ignoreEnv := ignoreClient, false
					if legacy {
						noClientEnv, ignoreEnv = false, ignoreClient
					}
					server := newLifecycleTestServer(t, Config{
						NoInheritEnv: noInherit,
						NoClientEnv:  noClientEnv,
						IgnoreEnv:    ignoreEnv,
						SessionRequestHandlers: map[string]SessionRequestHandler{
							"inspect-env": func(sess *Session, _ *Request) error {
								captured <- slices.Clone(sess.Env)
								return nil
							},
						},
					})
					listener := listenLifecycleTest(t)
					ctx, cancel := context.WithCancel(t.Context())
					stopped := make(chan error, 1)
					go func() { stopped <- server.StartWithContext(ctx, listener) }()
					t.Cleanup(func() {
						cancel()
						select {
						case err := <-stopped:
							if err != nil {
								t.Errorf("stop server: %v", err)
							}
						case <-time.After(5 * time.Second):
							t.Error("server did not stop")
						}
					})
					client := dialLifecycleTest(t, listener.Addr().String())
					defer client.Close()
					sess, err := client.NewSession()
					if err != nil {
						t.Fatal(err)
					}
					defer sess.Close()
					if err := sess.Setenv("SSHD_LITE_TEST_OVERRIDE", "client"); err != nil {
						t.Fatal(err)
					}
					if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
						t.Fatal(err)
					}
					if ok, err := sess.SendRequest("inspect-env", true, nil); err != nil || !ok {
						t.Fatalf("inspect environment: %v, %v", ok, err)
					}
					env := <-captured
					if slices.Contains(env, "SSHD_LITE_TEST_PROCESS=inherited") == noInherit {
						t.Error("process inheritance did not match NoInheritEnv")
					}
					if slices.Contains(env, "SSHD_LITE_TEST_OVERRIDE=client") == ignoreClient {
						t.Error("client environment did not match IgnoreEnv")
					}
					if !slices.Contains(env, "TERM=xterm") {
						t.Error("PTY TERM did not override inherited TERM")
					}
					for _, name := range []string{"SSH_CLIENT", "SSH_CONNECTION"} {
						count := 0
						for _, kv := range env {
							if strings.HasPrefix(kv, name+"=") {
								count++
								if kv == name+"=stale" {
									t.Errorf("%s retained inherited metadata", name)
								}
							}
						}
						if count != 1 {
							t.Errorf("%s has %d entries, want 1", name, count)
						}
					}
				})
			}
		}
	}
}
