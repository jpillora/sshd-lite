package display

// Mosh clients keep their local terminal in application cursor mode. The
// server converts unmodified SS3 arrows when its process requests normal mode.
// Keep state across datagrams, and deliver ESC immediately (including a lone
// Escape key); only the following 'O' needs one byte of lookahead.
type CursorKeys struct{ escape, ss3 bool }

func (k *CursorKeys) Translate(keys []byte, application bool) []byte {
	out := make([]byte, 0, len(keys)+1)
	for _, b := range keys {
		if k.ss3 {
			prefix := byte('O')
			if !application && b >= 'A' && b <= 'D' {
				prefix = '['
			}
			out = append(out, prefix, b)
			k.ss3 = false
			continue
		}
		if k.escape && b == 'O' {
			k.escape = false
			k.ss3 = true
			continue
		}
		k.escape = b == 0x1b
		out = append(out, b)
	}
	return out
}
