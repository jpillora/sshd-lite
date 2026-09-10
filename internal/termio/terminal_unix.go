//go:build !windows

package termio

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func OpenTTY() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

func OpenTTYOutput() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_WRONLY, 0) }

func WatchResize(ctx context.Context, f *os.File, resize func(int, int)) func() {
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
				cols, rows := Size(f)
				resize(cols, rows)
			}
		}
	}()
	return func() { signal.Stop(signals); cancel(); <-done }
}
