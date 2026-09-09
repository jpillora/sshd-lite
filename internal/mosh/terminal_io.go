package mosh

import (
	"context"
	"sync"

	"github.com/jpillora/sshd-lite/internal/mosh/display"
)

// terminalIO owns the terminal, emulator response pipe, and every I/O worker.
// The session loop alone writes/resizes the screen; responses are read by a pump.
type terminalIO struct {
	terminal      Terminal
	screen        *display.Screen
	input         chan []byte
	output        chan []byte
	exited        chan int
	ctx           context.Context
	cancel        context.CancelFunc
	stopResponses func() bool
	workers       sync.WaitGroup
}

func startTerminalIO(ctx context.Context, req Request, start StartTerminal, overloaded func()) (*terminalIO, error) {
	terminal, err := start()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	t := &terminalIO{terminal: terminal, screen: display.New(int(req.Cols), int(req.Rows)), input: make(chan []byte, 64), output: make(chan []byte, 64), exited: make(chan int, 1), ctx: ctx, cancel: cancel}
	// Cancellation closes the synchronous response pipe independently of the
	// session loop, which may itself be blocked writing a terminal query.
	t.stopResponses = context.AfterFunc(ctx, func() { t.screen.CloseResponses() })
	t.workers.Add(4)
	go t.readOutput()
	go t.writeInput()
	go t.readResponses(overloaded)
	go func() { defer t.workers.Done(); t.exited <- terminal.Wait() }()
	return t, nil
}

func (t *terminalIO) readOutput() {
	defer t.workers.Done()
	defer close(t.output)
	b := make([]byte, 8192)
	for {
		n, err := t.terminal.Read(b)
		if n > 0 {
			select {
			case t.output <- append([]byte(nil), b[:n]...):
			case <-t.ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}
func (t *terminalIO) writeInput() {
	defer t.workers.Done()
	for {
		select {
		case <-t.ctx.Done():
			return
		case b := <-t.input:
			if _, err := t.terminal.Write(b); err != nil {
				return
			}
		}
	}
}
func (t *terminalIO) readResponses(overloaded func()) {
	defer t.workers.Done()
	reader := t.screen.Responses()
	b := make([]byte, 1024)
	for {
		n, err := reader.Read(b)
		if n > 0 && !t.queueInput(append([]byte(nil), b[:n]...)) {
			// Never let a process issuing queries without reading answers block
			// the emulator, idle expiry, or server shutdown.
			overloaded()
			return
		}
		if err != nil {
			return
		}
	}
}
func (t *terminalIO) queueInput(b []byte) bool {
	select {
	case <-t.ctx.Done():
		return false
	default:
	}
	select {
	case t.input <- b:
		return true
	case <-t.ctx.Done():
		return false
	default:
		return false
	}
}

// stopProcess leaves the final screen available until protocol shutdown ends.
func (t *terminalIO) stopProcess() {
	t.cancel()
	t.screen.CloseResponses()
	_ = t.terminal.Close()
}
func (t *terminalIO) Close() {
	t.stopProcess()
	t.stopResponses()
	t.workers.Wait()
	t.screen.Close()
}
