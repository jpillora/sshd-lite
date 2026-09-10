package mosh

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jpillora/sshd-lite/internal/termio"
	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

const terminalModeReset = "\x1b[?1l\x1b[0m\x1b[?5l\x1b[?25h\x1b[?1000l\x1b[?1001l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?2004l"

const (
	alternateScreenEnter = "\x1b[?1049h"
	alternateScreenLeave = "\x1b[?1049l"
)

type terminalOutput struct {
	io.Writer
	tail      string
	alternate bool
}

func (w *terminalOutput) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
		data := w.tail + string(p[:n])
		enter, leave := strings.LastIndex(data, alternateScreenEnter), strings.LastIndex(data, alternateScreenLeave)
		if enter >= 0 || leave >= 0 {
			w.alternate = enter > leave
		}
		keep := len(alternateScreenEnter) - 1
		if len(data) > keep {
			data = data[len(data)-keep:]
		}
		w.tail = data
	}
	return n, err
}

func (w *terminalOutput) restoreScreen() {
	if w.alternate {
		_, _ = io.WriteString(w.Writer, alternateScreenLeave)
		w.alternate = false
	}
}

// Run serves an interactive local terminal over a dedicated SSH connection.
// It owns conn, restores terminal modes on exit, and handles Ctrl-^ . locally.
func Run(ctx context.Context, conn *ssh.Client, stdin *os.File, stdout io.Writer, config ClientConfig) (int, error) {
	defer conn.Close()
	cols, rows := termio.Size(stdin)
	output := stdout
	if term.IsTerminal(int(stdin.Fd())) {
		restore, err := termio.Raw(stdin)
		if err != nil {
			return 0, err
		}
		defer restore()
		if _, err := io.WriteString(stdout, "\x1b[?1h"); err != nil {
			return 0, err
		}
		// Restore modes changed by the remote application, but do not switch to
		// or clear an alternate screen merely because the connection started.
		defer io.WriteString(stdout, terminalModeReset)
		tracked := &terminalOutput{Writer: stdout}
		defer tracked.restoreScreen()
		output = tracked
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	config.Term, config.Columns, config.Rows, config.Output = os.Getenv("TERM"), cols, rows, output
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
