package display

import (
	"fmt"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

type Receiver struct {
	states      map[uint64]*State
	latest      uint64
	initialized bool
	scratch     *Screen
}

func NewReceiver(cols, rows int) *Receiver {
	scratch := New(cols, rows)
	scratch.CloseResponses()
	return &Receiver{states: map[uint64]*State{0: Blank(cols, rows)}, scratch: scratch}
}
func (s *Receiver) Apply(u *ssp.Update) ([]byte, error) {
	base, ok := s.states[u.OldNum]
	if !ok {
		return nil, fmt.Errorf("missing Screen base %d", u.OldNum)
	}
	messages, err := wire.UnmarshalHostMessage(u.Diff)
	if err != nil {
		return nil, err
	}
	next := base
	if len(messages) > 0 {
		s.scratch.Restore(base)
		for _, m := range messages {
			if m.Width != 0 || m.Height != 0 {
				if m.Width < 1 || m.Width > 1000 || m.Height < 1 || m.Height > 1000 || int64(m.Width)*int64(m.Height) > 100000 {
					return nil, fmt.Errorf("invalid remote Screen size")
				}
				s.scratch.Resize(int(m.Width), int(m.Height))
			}
			s.scratch.Write(m.Hoststring)
		}
		next = s.scratch.Snapshot()
	}
	s.states[u.NewNum] = next
	var out []byte
	if u.NewNum > s.latest {
		old := s.states[s.latest]
		if !s.initialized {
			// Render the initial state relative to a blank canvas of the remote
			// size. Ordinary shells therefore do not clear the receiving terminal;
			// a full-screen application can explicitly select its alternate screen.
			old = Blank(next.cols, next.rows)
			s.initialized = true
		}
		out = next.Diff(old)
		s.latest = u.NewNum
	}
	for n := range s.states {
		if n < u.ThrowawayNum {
			delete(s.states, n)
		}
	}
	// Bound retained Screen memory as well as transport state count. Identical
	// empty updates share a Snapshot and are charged once.
	seen := make(map[*State]bool)
	cells := 0
	var countState func(*State)
	countState = func(state *State) {
		if state == nil || seen[state] {
			return
		}
		seen[state] = true
		cells += len(state.cells)
		countState(state.primary)
	}
	for _, state := range s.states {
		countState(state)
	}
	if cells > 350000 {
		return nil, fmt.Errorf("Mosh retained Screen limit exceeded")
	}
	return out, nil
}

func (s *Receiver) Close() { s.scratch.Close() }
