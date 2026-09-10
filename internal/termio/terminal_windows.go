package termio

import (
	"context"
	"os"
	"time"
)

func OpenTTY() (*os.File, error) { return os.OpenFile("CONIN$", os.O_RDWR, 0) }

func OpenTTYOutput() (*os.File, error) { return os.OpenFile("CONOUT$", os.O_WRONLY, 0) }

func WatchResize(ctx context.Context, f *os.File, resize func(int, int)) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		cols, rows := Size(f)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c, r := Size(f)
				if c != cols || r != rows {
					cols, rows = c, r
					resize(c, r)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
