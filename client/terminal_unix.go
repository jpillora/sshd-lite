//go:build !windows

package client

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func openTTY() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

func watchResize(ctx context.Context, f *os.File, resize func(int, int)) func() {
	ctx, cancel := context.WithCancel(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				cols, rows := terminalSize(f)
				resize(cols, rows)
			}
		}
	}()
	return func() { signal.Stop(signals); cancel(); <-done }
}
