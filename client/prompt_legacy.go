package client

import "context"

// Connect keeps its established platform prompt support. Dial's typed adapter
// admits only the platforms with cancellable terminal ownership.
func legacyInteraction() Interaction {
	return Interaction{Passphrase: func(ctx context.Context, name string) ([]byte, error) {
		return legacyTerminalRead(ctx, "Private key passphrase: ", true)
	}}
}
