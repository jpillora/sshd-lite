// Package scenario provides data types and YAML parsing for SSH test scenarios.
package scenario

import (
	"fmt"
	"strings"
)

// Key represents special keys for SendKey action.
type Key string

const (
	KeyEnter     Key = "\r"
	KeyTab       Key = "\t"
	KeyEscape    Key = "\x1b"
	KeyBackspace Key = "\x7f"
	KeyCtrlC     Key = "\x03"
	KeyCtrlD     Key = "\x04"
	KeyCtrlZ     Key = "\x1a"
	KeyUp        Key = "\x1b[A"
	KeyDown      Key = "\x1b[B"
	KeyRight     Key = "\x1b[C"
	KeyLeft      Key = "\x1b[D"
)

// ParseKey converts a key name string to Key constant.
func ParseKey(name string) (Key, error) {
	switch strings.ToLower(name) {
	case "enter", "return":
		return KeyEnter, nil
	case "tab":
		return KeyTab, nil
	case "escape", "esc":
		return KeyEscape, nil
	case "backspace":
		return KeyBackspace, nil
	case "ctrl+c", "ctrlc":
		return KeyCtrlC, nil
	case "ctrl+d", "ctrld":
		return KeyCtrlD, nil
	case "ctrl+z", "ctrlz":
		return KeyCtrlZ, nil
	case "up":
		return KeyUp, nil
	case "down":
		return KeyDown, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
	default:
		return "", fmt.Errorf("unknown key: %s", name)
	}
}

// KeyLabel returns the human-readable name for a key.
func KeyLabel(k Key) string {
	switch k {
	case KeyEnter:
		return "Enter"
	case KeyTab:
		return "Tab"
	case KeyEscape:
		return "Escape"
	case KeyBackspace:
		return "Backspace"
	case KeyCtrlC:
		return "Ctrl+C"
	case KeyCtrlD:
		return "Ctrl+D"
	case KeyCtrlZ:
		return "Ctrl+Z"
	case KeyUp:
		return "Up"
	case KeyDown:
		return "Down"
	case KeyLeft:
		return "Left"
	case KeyRight:
		return "Right"
	default:
		return "unknown"
	}
}
