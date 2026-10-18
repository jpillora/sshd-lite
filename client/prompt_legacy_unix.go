//go:build unix

package client

import "context"

func legacyTerminalRead(ctx context.Context, prompt string, secret bool) ([]byte, error) {
	return terminalRead(ctx, prompt, secret)
}
