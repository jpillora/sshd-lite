package xssh

import (
	"encoding/binary"
	"fmt"
	"path/filepath"

	"github.com/jpillora/sshd-lite/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// registerSessionHandlers registers the built-in session request handlers
// when Session is enabled.
func (c *xconn) registerSessionHandlers() {
	if !c.config.Session {
		return
	}
	// Resolve shell path if the caller didn't. Safe to write: c.config is this
	// connection's own copy, not the one shared across connections.
	if !filepath.IsAbs(c.config.Shell) {
		if path, err := ShellPath(c.config.Shell); err == nil {
			c.config.Shell = path
		}
	}
	c.sessionRequestHandlers[PTYRequestType] = handlePtyReq
	c.sessionRequestHandlers[WindowChangeRequestType] = handleWindowChange
	c.sessionRequestHandlers[EnvRequestType] = handleEnv
	c.sessionRequestHandlers[ShellRequestType] = handleShell
	c.sessionRequestHandlers[ExecRequestType] = handleExec
}

// session logging helpers
func debugf(sess *Session, f string, args ...interface{}) {
	if sess.Logger != nil {
		sess.Logger.Debug(fmt.Sprintf(f, args...))
	}
}

func errorf(sess *Session, f string, args ...interface{}) {
	if sess.Logger != nil {
		sess.Logger.Error(fmt.Sprintf(f, args...))
	}
}

// handlePtyReq handles "pty-req" session requests
func handlePtyReq(sess *Session, req *Request) error {
	term, ws, err := parsePtyRequest(req.Payload)
	if err != nil {
		return err
	}
	// The terminal type belongs to the client's terminal, not the server's. It
	// used to be dropped, which left sessions running on whatever TERM the
	// operator's own shell happened to export.
	if term != "" {
		sess.Env = appendEnv(sess.Env, "TERM="+term)
	}
	queueResize(sess, ws)
	sess.pty = true
	debugf(sess, "PTY ready")
	return nil
}

// handleWindowChange handles "window-change" session requests
func handleWindowChange(sess *Session, req *Request) error {
	ws, err := parseWindowChange(req.Payload)
	if err != nil {
		return err
	}
	queueResize(sess, ws)
	return nil
}

type ptyRequestPayload struct {
	Term                    string
	Columns, Rows           uint32
	PixelWidth, PixelHeight uint32
	Modes                   string
}

type windowChangePayload struct {
	Columns, Rows           uint32
	PixelWidth, PixelHeight uint32
}

func parsePtyRequest(payload []byte) (string, *Winsize, error) {
	var p ptyRequestPayload
	if err := ssh.Unmarshal(payload, &p); err != nil {
		return "", nil, fmt.Errorf("malformed pty-req payload: %w", err)
	}
	if err := validateTerminalModes([]byte(p.Modes)); err != nil {
		return "", nil, fmt.Errorf("malformed pty-req terminal modes: %w", err)
	}
	ws, err := winsizeFromDimensions(p.Columns, p.Rows)
	if err != nil {
		return "", nil, fmt.Errorf("invalid pty-req dimensions: %w", err)
	}
	if !validTerm(p.Term) {
		return "", nil, fmt.Errorf("invalid pty-req terminal type")
	}
	return p.Term, ws, nil
}

// validTerm keeps a client-supplied terminal type to the shape TERM actually
// takes. It reaches a child process's environment, so it is bounded and
// restricted to printable ASCII rather than passed through verbatim.
func validTerm(term string) bool {
	if len(term) > 64 {
		return false
	}
	for _, r := range term {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '+':
		default:
			return false
		}
	}
	return true
}

func parseWindowChange(payload []byte) (*Winsize, error) {
	var p windowChangePayload
	if err := ssh.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("malformed window-change payload: %w", err)
	}
	ws, err := winsizeFromDimensions(p.Columns, p.Rows)
	if err != nil {
		return nil, fmt.Errorf("invalid window-change dimensions: %w", err)
	}
	return ws, nil
}

// validateTerminalModes checks the framing from RFC 4254 section 8. Opcodes
// 1-159 carry one uint32 argument. Opcode 0 terminates the stream. Opcodes
// 160-255 are undefined and cause parsing to stop, including when bytes remain
// in the modes string because their extension framing is unknown to us.
//
// An exhausted stream also ends parsing. TTY_OP_END is not required, because
// clients built on libssh2 send an empty modes string when the caller supplies
// no modes, and OpenSSH's own parser stops at the end of the buffer too.
// Rejecting those would fail pty-req for interoperable clients.
func validateTerminalModes(modes []byte) error {
	for len(modes) > 0 {
		opcode := modes[0]
		modes = modes[1:]
		switch {
		case opcode == 0:
			if len(modes) != 0 {
				return fmt.Errorf("%d trailing bytes after TTY_OP_END", len(modes))
			}
			return nil
		case opcode <= 159:
			if len(modes) < 4 {
				return fmt.Errorf("opcode %d has an incomplete uint32 argument", opcode)
			}
			modes = modes[4:]
		default:
			return nil
		}
	}
	return nil
}

// queueResize gives resize traffic latest-value semantics. A slow PTY resize
// consumer cannot stall request handling, and the next applied size is always
// the newest request observed by the dispatcher. A nil size represents RFC
// 4254's required handling for zero dimensions and is intentionally ignored.
func queueResize(sess *Session, ws *Winsize) {
	if ws == nil {
		return
	}
	payload := marshalDims(ws)
	select {
	case sess.Resizes <- payload:
		return
	default:
	}
	for {
		select {
		case <-sess.Resizes:
			continue
		default:
		}
		break
	}
	select {
	case sess.Resizes <- payload:
	default:
		// A direct external producer may have won the slot. Requests handled
		// by the built-in dispatcher are serialized and do not take this path.
	}
}

// handleEnv handles "env" session requests
func handleEnv(sess *Session, req *Request) error {
	e := struct{ Name, Value string }{}
	if err := ssh.Unmarshal(req.Payload, &e); err != nil {
		return fmt.Errorf("failed to unmarshal env: %w", err)
	}
	kv := e.Name + "=" + e.Value
	debugf(sess, "env: %s", kv)
	if !sess.Config().NoClientEnv && !sess.Config().IgnoreEnv {
		sess.Env = appendEnv(sess.Env, kv)
	}
	return nil
}

// handleShell handles "shell" session requests
func handleShell(sess *Session, req *Request) error {
	if len(req.Payload) > 0 {
		debugf(sess, "shell command ignored '%s'", req.Payload)
	}
	return attachShell(sess)
}

// handleExec handles "exec" session requests
func handleExec(sess *Session, req *Request) error {
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	// command name is a string encoded as: [uint32 length][string command]
	if len(req.Payload) < 4 {
		return fmt.Errorf("malformed exec request payload")
	}
	length := binary.BigEndian.Uint32(req.Payload)
	if uint32(len(req.Payload)-4) != length {
		return fmt.Errorf("command length mismatch in payload")
	}
	command := string(req.Payload[4:])
	debugf(sess, "exec command: %s", command)

	// A command can fail and close its channel immediately. Acknowledge the
	// request before starting it so exit-status and channel-close packets cannot
	// overtake the request reply and surface as EOF from ssh.Session.Start.
	if req.WantReply {
		if err := req.Reply(true, nil); err != nil {
			return fmt.Errorf("failed to accept exec request: %w", err)
		}
	}

	// Execute the command.
	sess.goTask(func() { executeCommand(sess, command) })
	return nil
}

// shellBase returns the shell's name without directory or executable suffix,
// lowercased. Windows shells arrive as paths like C:\WINDOWS\system32\cmd.exe,
// so comparing the raw basename would miss every one of them. Both separators
// are honoured on every platform rather than using filepath.Base, which does
// not treat \ as a separator off Windows.
func shellBase(shell string) string { return terminal.ShellBase(shell) }

// commandFlag returns the flag that makes shell run a single command string.
// cmd.exe is the odd one out: it takes /c, where POSIX shells and PowerShell
// (for which -c abbreviates -Command) all take -c. Passing -c to cmd.exe does
// not fail loudly — cmd.exe waits for input that never comes, so the session
// hangs until the client gives up.
func commandFlag(shell string) string {
	if shellBase(shell) == "cmd" {
		return "/c"
	}
	return "-c"
}
