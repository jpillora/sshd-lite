package mosh

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"

	protocol "github.com/jpillora/sshd-lite/internal/mosh"
	"github.com/jpillora/sshd-lite/xssh"
)

// Attach starts UDP on the SSH listener's address and numeric port, returning
// an xssh exec handler for standard mosh-server bootstrap and a shutdown closer.
// Set sshd.Config.Attach to this function, or install the handler directly in
// xssh.Config.ExecHandler and own the closer when using xssh independently.
func Attach(ctx context.Context, addr net.Addr) (xssh.ExecHandler, io.Closer, error) {
	server, err := protocol.Listen(ctx, addr)
	if err != nil {
		return nil, nil, err
	}
	return bootstrapHandler(server), server, nil
}

// RejectAttach installs a bootstrap handler which reports that embedded Mosh
// is disabled without opening a UDP listener. The sshd-lite binary uses it for
// listeners started without --mosh; SSH-only library users need not link Mosh.
func RejectAttach(context.Context, net.Addr) (xssh.ExecHandler, io.Closer, error) {
	return func(sess *xssh.Session, command string) (bool, error) {
		_, handled, _ := protocol.ParseBootstrap(command)
		if !handled {
			return false, nil
		}
		defer sess.Channel.Close()
		fmt.Fprintln(sess.Channel.Stderr(), "sshd-lite: Mosh disabled; restart the server with --mosh")
		exitStatus(sess, 1)
		return true, nil
	}, nil, nil
}

func exitStatus(sess *xssh.Session, code uint32) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, code)
	_, _ = sess.Channel.SendRequest("exit-status", false, b)
}

func bootstrapHandler(server *protocol.Server) xssh.ExecHandler {
	return func(sess *xssh.Session, command string) (bool, error) {
		bootstrap, handled, err := protocol.ParseBootstrap(command)
		if !handled {
			return false, nil
		}
		defer sess.Channel.Close()
		exit := func(code uint32) { exitStatus(sess, code) }
		if err == nil {
			err = bootstrap.ValidateListener(server.Port(), sess.Conn().LocalAddr())
		}
		if err != nil {
			fmt.Fprintln(sess.Channel.Stderr(), err)
			exit(1)
			return true, nil
		}
		if bootstrap.Help || bootstrap.Version {
			fmt.Fprintln(sess.Channel, "sshd-lite embedded mosh-server (Mosh protocol 2)")
			exit(0)
			return true, nil
		}
		select {
		case dims := <-sess.Resizes:
			if len(dims) == 8 {
				cols, rows := binary.BigEndian.Uint32(dims[:4]), binary.BigEndian.Uint32(dims[4:])
				if cols > 0 && cols <= 1000 && rows > 0 && rows <= 1000 {
					bootstrap.Cols = uint16(cols)
					bootstrap.Rows = uint16(rows)
				}
			}
		default:
		}
		// Retain only terminal launch policy and peer addresses. The pending UDP
		// session must not keep the SSH session, connection or handler maps alive.
		cfg := sess.Config()
		launch := xssh.Config{
			Shell: cfg.Shell, WorkingDirectory: cfg.WorkingDirectory,
			NoClientEnv: cfg.NoClientEnv || cfg.IgnoreEnv, NoInheritEnv: cfg.NoInheritEnv, NoGlobalEnv: cfg.NoGlobalEnv,
		}
		remote, local := sess.Conn().RemoteAddr(), sess.Conn().LocalAddr()
		credentials, revoke, err := server.Issue(bootstrap.Request, func() (protocol.Terminal, error) {
			return xssh.StartTerminalCommand(&launch, remote, local, bootstrap.Term, bootstrap.Cols, bootstrap.Rows, bootstrap.Command, bootstrap.Env)
		})
		if err != nil {
			fmt.Fprintln(sess.Channel.Stderr(), err)
			exit(1)
			return true, nil
		}
		if bootstrap.PrintConnection {
			for _, kv := range sess.Env {
				if strings.HasPrefix(kv, "SSH_CONNECTION=") {
					fmt.Fprintf(sess.Channel, "\nMOSH SSH_CONNECTION %s\n", strings.TrimPrefix(kv, "SSH_CONNECTION="))
					break
				}
			}
		}
		if _, err := fmt.Fprintf(sess.Channel, "\nMOSH CONNECT %d %s\n", credentials.Port, strings.TrimRight(credentials.Key, "=")); err != nil {
			revoke()
			return true, err
		}
		exit(0)
		return true, nil
	}
}
