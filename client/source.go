package client

import (
	"context"
	"errors"
	"io"
	"os"
)

// readSource supplies bounded, stable regular-file snapshots to configuration,
// identity loading and trust. Platform openSource implementations reject devices
// without blocking on Unix. Missing files are optional to each policy owner.
func readSource(ctx context.Context, name string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openSource(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("ssh client: nonregular or oversized source")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("ssh client: source byte limit")
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || int64(len(b)) != info.Size() {
		return nil, errors.New("ssh client: source changed while reading")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}
