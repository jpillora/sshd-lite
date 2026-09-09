package xssh

import (
	"net"

	"github.com/jpillora/sshd-lite/internal/terminal"
)

// Terminal is a process-backed PTY for transports with a lifecycle independent
// of an SSH channel. It uses the same shell, workdir and environment policy.
type Terminal struct{ *terminal.Process }

func StartTerminal(cfg *Config, remote, local net.Addr, term string, cols, rows uint16) (*Terminal, error) {
	return StartTerminalCommand(cfg, remote, local, term, cols, rows, nil, nil)
}

// StartTerminalCommand starts a shell or a literal argv command in a PTY.
func StartTerminalCommand(cfg *Config, remote, local net.Addr, term string, cols, rows uint16, argv, extraEnv []string) (*Terminal, error) {
	cmd := terminal.Command(cfg.Shell, argv)
	cmd.Dir = cfg.WorkingDirectory
	env, err := sessionEnv(cfg.NoInheritEnv, cfg.NoGlobalEnv, systemEnvFile)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(env, connectionEnv(remote, local)...)
	if term == "" {
		term = "xterm-256color"
	}
	if !cfg.IgnoreEnv {
		for _, kv := range extraEnv {
			cmd.Env = appendEnv(cmd.Env, kv)
		}
	}
	cmd.Env = appendEnv(cmd.Env, "TERM="+term)
	p, err := terminal.Start(cmd, &Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &Terminal{Process: p}, nil
}
