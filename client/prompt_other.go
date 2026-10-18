//go:build !unix

package client

import "context"

func terminalRead(ctx context.Context, prompt string, secret bool) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, ErrInteractionRequired
}
