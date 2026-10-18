//go:build unix

package client

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"io"
)

func terminalRead(ctx context.Context, prompt string, secret bool) (result []byte, err error) {
	fd, e := unix.Open("/dev/tty", unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if e != nil {
		return nil, fmt.Errorf("%w: no terminal", ErrInteractionRequired)
	}
	defer unix.Close(fd)
	return readTerminal(ctx, fd, prompt, secret)
}
func readTerminal(ctx context.Context, fd int, prompt string, secret bool) (result []byte, err error) {
	state, e := term.MakeRaw(fd)
	if e != nil {
		return nil, e
	}
	defer func() {
		err = errors.Join(err, term.Restore(fd, state))
		if err != nil {
			clear(result)
			result = nil
		}
	}()
	write := func(b []byte) error {
		for len(b) > 0 {
			if e := terminalReady(ctx, fd, unix.POLLOUT); e != nil {
				return e
			}
			n, e := unix.Write(fd, b)
			if e == unix.EAGAIN || e == unix.EINTR {
				continue
			}
			if e != nil {
				return e
			}
			b = b[n:]
		}
		return nil
	}
	if e := write([]byte(prompt)); e != nil {
		return nil, e
	}
	buf := make([]byte, 0, 4096)
	defer func() {
		if err != nil {
			clear(buf)
		}
	}()
	var one [1]byte
	for {
		if e := terminalReady(ctx, fd, unix.POLLIN); e != nil {
			return nil, e
		}
		n, e := unix.Read(fd, one[:])
		if e == unix.EAGAIN || e == unix.EINTR {
			continue
		}
		if e != nil {
			return nil, e
		}
		if n == 0 {
			return nil, io.EOF
		}
		c := one[0]
		switch c {
		case '\r', '\n':
			if e := write([]byte("\r\n")); e != nil {
				return nil, e
			}
			return buf, nil
		case 3:
			return nil, context.Canceled
		case 4:
			return nil, io.EOF
		case 127, 8:
			if len(buf) > 0 {
				buf[len(buf)-1] = 0
				buf = buf[:len(buf)-1]
				if !secret {
					if e := write([]byte("\b \b")); e != nil {
						return nil, e
					}
				}
			}
			continue
		}
		if c < 32 {
			continue
		}
		if len(buf) >= 4096 {
			return nil, errors.New("ssh client: terminal input limit")
		}
		buf = append(buf, c)
		if !secret {
			if e := write(one[:]); e != nil {
				return nil, e
			}
		}
	}
}
func terminalReady(ctx context.Context, fd int, event int16) error {
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		p := []unix.PollFd{{Fd: int32(fd), Events: event}}
		n, e := unix.Poll(p, 100)
		if e == unix.EINTR {
			continue
		}
		if e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		if p[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return errors.New("ssh client: terminal closed")
		}
		if p[0].Revents&(event|unix.POLLHUP) != 0 {
			return nil
		}
	}
}
