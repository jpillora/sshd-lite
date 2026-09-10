package mosh

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalCleanupDoesNotClearOrSwitchScreens(t *testing.T) {
	for _, forbidden := range []string{"\x1b[2J", "\x1b[?1049h", "\x1b[?1049l"} {
		if strings.Contains(terminalModeReset, forbidden) {
			t.Fatalf("terminal cleanup contains screen-changing sequence %q", forbidden)
		}
	}
}

func TestTerminalOutputRestoresOnlyAnActiveAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	tracked := &terminalOutput{Writer: &out}
	if _, err := tracked.Write([]byte("prefix\x1b[?10")); err != nil {
		t.Fatal(err)
	}
	if _, err := tracked.Write([]byte("49hremote")); err != nil {
		t.Fatal(err)
	}
	tracked.restoreScreen()
	if !strings.HasSuffix(out.String(), alternateScreenLeave) {
		t.Fatalf("active alternate screen was not restored: %q", out.String())
	}
	before := out.Len()
	tracked.restoreScreen()
	if out.Len() != before {
		t.Fatal("inactive alternate screen emitted a restore sequence")
	}
}
