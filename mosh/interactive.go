package mosh

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/jpillora/sshd-lite/internal/termio"
	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Run serves an interactive local terminal over a dedicated SSH connection.
// It owns conn, restores terminal modes on exit, and handles Ctrl-^ . locally.
func Run(ctx context.Context, conn *ssh.Client, stdin *os.File, stdout io.Writer, config ClientConfig) (int, error) {
	defer conn.Close()
	cols, rows := termio.Size(stdin)
	if term.IsTerminal(int(stdin.Fd())) {
		restore, err := termio.Raw(stdin)
		if err != nil {
			return 0, err
		}
		defer restore()
		// Isolate the remote screen from local shell contents, and restore the
		// local display as well as termios when the Mosh session ends.
		if _, err := io.WriteString(stdout, "\x1b[?1049h\x1b[?1h\x1b[0m\x1b[H\x1b[2J"); err != nil {
			return 0, err
		}
		defer io.WriteString(stdout, "\x1b[?1l\x1b[0m\x1b[?5l\x1b[?25h\x1b[?1000l\x1b[?1001l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?2004l\x1b[?1049l")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	config.Term, config.Columns, config.Rows, config.Output = os.Getenv("TERM"), cols, rows, stdout
	session, err := Start(ctx, conn, config)
	if err != nil {
		return 0, err
	}
	defer session.Close()
	var source io.Reader = stdin
	if info, statErr := stdin.Stat(); statErr == nil && (info.Mode().IsRegular() || (info.Mode()&os.ModeCharDevice != 0 && !term.IsTerminal(int(stdin.Fd())))) {
		// Regular files and /dev/null cannot be registered with epoll. Their reads
		// complete without needing cancellation, so hide the descriptor interface.
		source = struct{ io.Reader }{stdin}
	}
	reader, err := cancelreader.NewReader(source)
	if err != nil {
		return 0, err
	}
	readDone := make(chan struct{})
	var escaped atomic.Bool
	defer func() {
		cancel()
		if reader.Cancel() {
			<-readDone
		} else {
			// Some Windows console drivers cannot cancel an overlapped read. Do not
			// hold terminal restoration hostage to a reader the OS cannot interrupt.
			select {
			case <-readDone:
			case <-time.After(100 * time.Millisecond):
			}
		}
		_ = reader.Close()
	}()
	go func() {
		defer close(readDone)
		b := make([]byte, 4096)
		escape := false
		for {
			n, err := reader.Read(b)
			if n > 0 {
				keys := make([]byte, 0, n)
				for _, key := range b[:n] {
					if escape {
						escape = false
						if key == '.' {
							escaped.Store(true)
							cancel()
							return
						}
						keys = append(keys, 0x1e)
						if key == 0x1e {
							continue
						}
					} else if key == 0x1e {
						escape = true
						continue
					}
					keys = append(keys, key)
				}
				if _, err := session.Write(keys); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	stopResize := termio.WatchResize(ctx, stdin, func(cols, rows int) { _ = session.Resize(cols, rows) })
	defer stopResize()
	code, err := session.Wait()
	if escaped.Load() && errors.Is(err, context.Canceled) {
		return 0, nil
	}
	return code, err
}
