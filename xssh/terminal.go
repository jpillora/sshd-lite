package xssh

import (
	"net"
	"os/exec"
	"sync"
)

// Terminal is a process-backed PTY for transports with a lifecycle independent
// of an SSH channel. It uses the same shell, workdir and environment policy.
type Terminal struct {
	PTY
	cmd    *exec.Cmd
	done   chan struct{}
	code   int
	mu     sync.Mutex
	closed bool
}

func StartTerminal(cfg *Config, remote, local net.Addr, term string, cols, rows uint16) (*Terminal, error) {
	args := []string{}
	switch shellBase(cfg.Shell) {
	case "bash", "fish":
		args = append(args, "-l")
	}
	cmd := exec.Command(cfg.Shell, args...)
	setSysProcAttr(cmd)
	cmd.Dir = cfg.WorkingDirectory
	env, err := sessionEnv(cfg.NoInheritEnv, cfg.NoGlobalEnv, systemEnvFile)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(env, connectionEnv(remote, local)...)
	if term == "" {
		term = "xterm-256color"
	}
	cmd.Env = appendEnv(cmd.Env, "TERM="+term)
	p, err := startPTY(cmd, &Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	t := &Terminal{PTY: p, cmd: cmd, done: make(chan struct{})}
	go func() {
		state, err := cmd.Process.Wait()
		t.code = 1
		if err == nil {
			t.code = int(shellExitCode(state))
		}
		close(t.done)
	}()
	return t, nil
}

func (t *Terminal) Wait() int { <-t.done; return t.code }

func (t *Terminal) Resize(cols, rows uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || !supportsRunningPTYResize {
		return nil
	}
	return SetWinsize(t.PTY, uint32(cols), uint32(rows))
}

func (t *Terminal) Close() error {
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		_ = t.cmd.Process.Kill()
		closeShellPTY(t.PTY)
	}
	t.mu.Unlock()
	<-t.done
	return nil
}
