package terminal

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Command selects the configured login shell or a literal program argv.
func Command(shell string, argv []string) *exec.Cmd {
	if len(argv) > 0 {
		return exec.Command(argv[0], argv[1:]...)
	}
	var args []string
	switch ShellBase(shell) {
	case "bash", "fish":
		args = []string{"-l"}
	}
	return exec.Command(shell, args...)
}
func ShellBase(shell string) string {
	if i := strings.LastIndexAny(shell, `/\`); i >= 0 {
		shell = shell[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(shell), ".exe")
}
func ExitCode(state *os.ProcessState) int {
	if state != nil && state.ExitCode() >= 0 {
		return state.ExitCode()
	}
	return 1
}

// Process owns a PTY and exactly one process reaper. Protocol adapters decide
// when to stop a process and how long to await Done; they never call Process.Wait
// on the underlying os.Process themselves. Release serializes close with resize.
type Process struct {
	PTY
	cmd      *exec.Cmd
	done     chan struct{}
	state    *os.ProcessState
	err      error
	mu       sync.Mutex
	released bool
}

func Start(cmd *exec.Cmd, size *Size) (*Process, error) {
	prepare(cmd)
	pty, err := startPTY(cmd, size)
	if err != nil {
		return nil, err
	}
	p := &Process{PTY: pty, cmd: cmd, done: make(chan struct{})}
	go func() { p.state, p.err = cmd.Process.Wait(); close(p.done) }()
	return p, nil
}
func (p *Process) Done() <-chan struct{}             { return p.done }
func (p *Process) Result() (*os.ProcessState, error) { <-p.done; return p.state, p.err }
func (p *Process) Wait() int                         { state, _ := p.Result(); return ExitCode(state) }
func (p *Process) Signal(signal os.Signal) error     { return p.cmd.Process.Signal(signal) }
func (p *Process) Kill() error                       { return p.cmd.Process.Kill() }
func (p *Process) Resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released || !supportsRunningPTYResize {
		return nil
	}
	size, err := Dimensions(uint32(cols), uint32(rows))
	if err != nil || size == nil {
		return err
	}
	return setSize(p.PTY, size)
}
func (p *Process) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released {
		closeShellPTY(p.PTY)
		p.released = true
	}
}

// Close kills the process, releases its PTY and awaits the reaper. An adapter
// requiring bounded shutdown uses Kill/Release and selects on Done instead.
func (p *Process) Close() error { _ = p.Kill(); p.Release(); <-p.done; return nil }
