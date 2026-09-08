package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/jpillora/sshd-lite/internal/mosh"
	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func runMosh(ctx context.Context, conn *ssh.Client, stdin *os.File, stdout io.Writer) (int, error) {
	cols, rows := terminalSize(stdin)
	request := mosh.Request{Term: os.Getenv("TERM"), Cols: uint16(cols), Rows: uint16(rows)}
	payload, _ := json.Marshal(request)
	// Bound bootstrap independently of the lifetime of the UDP session.
	timer := time.AfterFunc(10*time.Second, func() { conn.Close() })
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	ok, reply, err := conn.SendRequest(mosh.RequestName, true, payload)
	timer.Stop()
	stop()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("server rejected Mosh; enable --mosh on sshd-lite")
	}
	var credentials mosh.Credentials
	if err := json.Unmarshal(reply, &credentials); err != nil {
		return 0, err
	}
	// Use the actual SSH peer, avoiding DNS resolving differently for UDP.
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return 0, err
	}
	conn.Close()
	udp, err := (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(credentials.Port)))
	if err != nil {
		return 0, err
	}
	defer udp.Close()
	if term.IsTerminal(int(stdin.Fd())) {
		restore, err := rawTerminal(stdin)
		if err != nil {
			return 0, err
		}
		defer restore()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	input := make(chan []byte, 16)
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
		defer close(input)
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
				select {
				case input <- keys:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	sizes := make(chan mosh.Request, 1)
	stopResize := watchResize(ctx, stdin, func(cols, rows int) {
		select {
		case <-sizes:
		default:
		}
		sizes <- mosh.Request{Cols: uint16(cols), Rows: uint16(rows)}
	})
	defer stopResize()
	return mosh.RunClient(ctx, udp, credentials.Key, input, sizes, stdout)
}
