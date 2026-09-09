package mosh

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Bootstrap is the virtual mosh-server invocation accepted over SSH exec.
type Bootstrap struct {
	Request
	Port            string
	Bind            string
	SSHBind         bool
	Colors          int
	Env             []string
	Command         []string
	PrintConnection bool
	Help, Version   bool
}

const connectionProbe = `[ -n "$SSH_CONNECTION" ] && printf "\nMOSH SSH_CONNECTION %s\n" "$SSH_CONNECTION"`

func literal(w *syntax.Word) (string, error) {
	if len(w.Parts) > 0 {
		if v, ok := w.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(v.Value, "~") {
			return "", fmt.Errorf("tilde expansion is not supported")
		}
	}
	var parts func([]syntax.WordPart) error
	parts = func(ps []syntax.WordPart) error {
		for _, p := range ps {
			switch v := p.(type) {
			case *syntax.Lit:
				// Literal text; quoting is decoded only after validation.
			case *syntax.SglQuoted:
				// Literal text; quoting is decoded only after validation.
			case *syntax.DblQuoted:
				if err := parts(v.Parts); err != nil {
					return err
				}
			default:
				return fmt.Errorf("mosh-server arguments must be literal")
			}
		}
		return nil
	}
	if err := parts(w.Parts); err != nil {
		return "", err
	}
	// A nil Config shares mutable package-global expansion buffers. Each
	// authenticated session must own its buffers when bootstraps run concurrently.
	fields, err := expand.Fields(&expand.Config{}, w)
	if err != nil {
		return "", err
	}
	if len(fields) != 1 {
		return "", fmt.Errorf("expected one literal argument")
	}
	return fields[0], nil
}
func call(stmt *syntax.Stmt) ([]string, error) {
	c, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || stmt.Background || stmt.Negated || stmt.Coprocess || len(stmt.Redirs) > 0 {
		return nil, fmt.Errorf("unsupported mosh-server command structure")
	}
	for _, a := range c.Assigns {
		if a.Name == nil || a.Name.Value != "MOSH_SERVER_NETWORK_TMOUT" || a.Value == nil {
			return nil, fmt.Errorf("unsupported mosh-server assignment")
		}
		if _, err := literal(a.Value); err != nil {
			return nil, err
		}
	}
	var args []string
	for _, w := range c.Args {
		v, err := literal(w)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	return args, nil
}

// ParseBootstrap recognizes executable invocations, never substring matches or
// shell evaluation. The one supported compound form is Debian's IP probe.
func ParseBootstrap(command string) (*Bootstrap, bool, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, false, nil
	}
	if len(f.Stmts) == 0 {
		return nil, false, nil
	}
	b := &Bootstrap{Request: Request{Term: "xterm-256color", Cols: 80, Rows: 24}, Colors: 256}
	stmts := f.Stmts
	if len(stmts) == 2 {
		args, err := call(stmts[0])
		if err != nil || len(args) != 3 || args[0] != "sh" || args[1] != "-c" || args[2] != connectionProbe {
			return nil, false, nil
		}
		b.PrintConnection = true
		stmts = stmts[1:]
	}
	if len(stmts) != 1 {
		return nil, false, nil
	}
	args, err := call(stmts[0])
	if err != nil {
		return nil, false, nil
	}
	if len(args) == 0 || args[0] != "mosh-server" {
		return nil, false, nil
	}
	fail := func(format string, args ...any) (*Bootstrap, bool, error) {
		return nil, true, fmt.Errorf(format, args...)
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "--version") {
		b.Help = args[1] == "--help"
		b.Version = !b.Help
		return b, true, nil
	}
	if len(args) < 2 || args[1] != "new" {
		return fail("usage: mosh-server new [-s] [-c COLORS] [-l NAME=VALUE] [-p PORT[:PORT2]] [-- COMMAND...]")
	}
	for i := 2; i < len(args); i++ {
		flag := args[i]
		if flag == "--" {
			b.Command = append([]string(nil), args[i+1:]...)
			break
		}
		switch flag {
		case "-s":
			b.SSHBind = true
		case "-v": // accepted; sshd-lite's logger owns verbosity
		case "-c", "-l", "-p", "-i":
			i++
			if i >= len(args) {
				return fail("%s needs an argument", flag)
			}
			v := args[i]
			switch flag {
			case "-c":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return fail("invalid color count")
				}
				b.Colors = n
				if n != 256 {
					b.Term = "xterm"
				}
			case "-l":
				name, _, ok := strings.Cut(v, "=")
				if !ok || !(name == "LANG" || name == "LANGUAGE" || strings.HasPrefix(name, "LC_")) {
					return fail("only locale assignments are supported by -l")
				}
				b.Env = append(b.Env, v)
			case "-p":
				b.Port = v
			case "-i":
				if net.ParseIP(v) == nil {
					return fail("invalid bind address")
				}
				b.Bind = v
			}
		default:
			return fail("unsupported mosh-server option %q", flag)
		}
	}
	return b, true, nil
}

func (b *Bootstrap) ValidateListener(port int, local net.Addr) error {
	if b.Port != "" {
		first, last, ranged := strings.Cut(b.Port, ":")
		lo, err := strconv.Atoi(first)
		if err != nil || lo < 1 || lo > 65535 {
			return fmt.Errorf("invalid UDP port")
		}
		hi := lo
		if ranged {
			hi, err = strconv.Atoi(last)
			if err != nil || hi < lo || hi > 65535 {
				return fmt.Errorf("invalid UDP port range")
			}
		}
		if port < lo || port > hi {
			return fmt.Errorf("sshd-lite shares TCP/UDP port %d; requested range does not include it", port)
		}
	}
	if b.Bind != "" {
		host, _, err := net.SplitHostPort(local.String())
		if err != nil || !net.ParseIP(host).Equal(net.ParseIP(b.Bind)) {
			return fmt.Errorf("requested bind address differs from SSH listener")
		}
	}
	return nil
}
