package display

import (
	"fmt"
	"io"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	vt "github.com/charmbracelet/x/vt"
)

// State is the terminal state carried by Mosh HostMessage updates. Each
// update is applied to its named base, not to whatever is currently displayed.
// Keep complete cell contents (including combining marks), pen, cursor and the
// modes emitted by Mosh's display protocol. No scrollback history is retained.
type State struct {
	cols, rows, x, y int
	cells            []uv.Cell
	pen              uv.Style
	modes            map[int]bool
	title            string
	bell             uint64
}

var screenModes = []int{5, 25, 1000, 1001, 1002, 1003, 1004, 1005, 1006, 1015, 2004}

func Blank(cols, rows int) *State {
	s := &State{cols: cols, rows: rows, cells: make([]uv.Cell, cols*rows), modes: map[int]bool{25: true}}
	for i := range s.cells {
		s.cells[i] = uv.EmptyCell
	}
	return s
}

// Screen owns one reusable emulator. Its Snapshot is independent of later
// writes, so state retention costs Screen cells rather than emulator parsers.
type Screen struct {
	applicationCursor bool
	emu               *vt.Emulator
	parser            *ansi.Parser
	pen               uv.Style
	modes             map[int]bool
	title             string
	bell              uint64
}

func New(cols, rows int) *Screen {
	s := &Screen{emu: vt.NewEmulator(cols, rows), modes: map[int]bool{25: true}}
	s.emu.SetScrollbackSize(0)
	s.emu.SetCallbacks(vt.Callbacks{
		Title: func(v string) { s.title = v }, Bell: func() { s.bell++ },
		CursorVisibility: func(v bool) { s.modes[25] = v },
		EnableMode:       func(m ansi.Mode) { s.mode(m, true) }, DisableMode: func(m ansi.Mode) { s.mode(m, false) },
	})
	s.parser = ansi.NewParser()
	s.parser.SetHandler(ansi.Handler{HandleCsi: func(cmd ansi.Cmd, p ansi.Params) {
		if cmd.Final() == 'm' && cmd.Prefix() == 0 && cmd.Intermediate() == 0 {
			uv.ReadStyle(p, &s.pen)
		}
	}, HandleEsc: func(cmd ansi.Cmd) {
		if cmd.Final() == 'c' {
			s.pen = uv.Style{}
			s.applicationCursor = false
			s.modes = map[int]bool{25: true}
		}
	}})
	return s
}
func (s *Screen) mode(mode ansi.Mode, on bool) {
	m, ok := mode.(ansi.DECMode)
	if !ok {
		return
	}
	if int(m) == 1 {
		s.applicationCursor = on
		return
	}
	for _, n := range screenModes {
		if int(m) == n {
			s.modes[n] = on
			return
		}
	}
}
func (s *Screen) Write(b []byte) {
	for _, c := range b {
		s.parser.Advance(c)
	}
	_, _ = s.emu.Write(b)
}
func (s *Screen) Snapshot() *State {
	state := Blank(s.emu.Width(), s.emu.Height())
	p := s.emu.CursorPosition()
	state.x, state.y = p.X, p.Y
	state.pen = s.pen
	state.title = s.title
	state.bell = s.bell
	for _, n := range screenModes {
		state.modes[n] = s.modes[n]
	}
	for y := 0; y < state.rows; y++ {
		for x := 0; x < state.cols; x++ {
			if c := s.emu.CellAt(x, y); c != nil {
				state.cells[y*state.cols+x] = *c.Clone()
				if c.Content == "" && c.Width == 0 && (x == 0 || state.cells[y*state.cols+x-1].Width < 2) {
					state.cells[y*state.cols+x].Content = " "
					state.cells[y*state.cols+x].Width = 1
				}
			}
		}
	}
	return state
}
func (s *Screen) Restore(state *State) {
	// Mosh's wire updates are complete ANSI sequences, with a normalized cursor;
	// they do not expose a server's private parser, scrolling region or alt buffer.
	s.emu.Resize(state.cols, state.rows)
	s.Write([]byte("\x1bc"))
	s.Write(state.Diff(nil))
	s.title = state.title
	s.bell = state.bell
}
func (s *Screen) Close()          { _ = s.emu.Close() }
func (s *Screen) CloseResponses() { _ = s.emu.InputPipe().(io.Closer).Close() }

func (s *State) Diff(old *State) []byte {
	var b strings.Builder
	var pen uv.Style
	penKnown := false
	full := old == nil || old.cols != s.cols || old.rows != s.rows
	if full {
		b.WriteString("\x1b[0m\x1b[r\x1b[H\x1b[2J")
	}
	// Rendering never depends on the receiver's current pen or wrap-pending bit.
	for y := 0; y < s.rows; y++ {
		writing := false
		for x := 0; x < s.cols; x++ {
			c := s.cells[y*s.cols+x]
			if c.Width == 0 && c.Content != "" {
				continue
			}
			if !full && c.Equal(&old.cells[y*s.cols+x]) {
				writing = false
				continue
			}
			if !writing {
				fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, x+1)
			}
			if !penKnown || !pen.Equal(&c.Style) {
				b.WriteString("\x1b[0m")
				b.WriteString(c.Style.String())
				pen = c.Style
				penKnown = true
			}
			if c.Content == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(c.Content)
			}
			writing = true
			if c.Width > 1 {
				x += c.Width - 1
			}
		}
	}
	if full || old.x != s.x || old.y != s.y || b.Len() > 0 {
		fmt.Fprintf(&b, "\x1b[%d;%dH", s.y+1, s.x+1)
	}
	if full || !s.pen.Equal(&old.pen) || b.Len() > 0 {
		b.WriteString("\x1b[0m")
		b.WriteString(s.pen.String())
	}
	for _, m := range screenModes {
		if full || old.modes[m] != s.modes[m] {
			last := 'l'
			if s.modes[m] {
				last = 'h'
			}
			fmt.Fprintf(&b, "\x1b[?%d%c", m, last)
		}
	}
	if full || s.title != old.title {
		fmt.Fprintf(&b, "\x1b]2;%s\x07", s.title)
	}
	if old != nil && s.bell != old.bell {
		b.WriteByte(7)
	}
	return []byte(b.String())
}

// Responses reads locally generated terminal replies; CloseResponses unblocks it.
func (s *Screen) Responses() io.Reader    { return s.emu }
func (s *Screen) Resize(cols, rows int)   { s.emu.Resize(cols, rows) }
func (s *Screen) ApplicationCursor() bool { return s.applicationCursor }
func (s *State) Columns() int             { return s.cols }
func (s *State) Rows() int                { return s.rows }
