package mosh

import (
	"fmt"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

// Each resize is one action; each input byte is one action, matching Mosh's
// UserStream. Absolute offsets allow old states to be pruned without retaining
// a session's whole input history.
type inputStates struct {
	ends      map[uint64]uint64
	delivered uint64
}

func newInputStates() *inputStates { return &inputStates{ends: map[uint64]uint64{0: 0}} }
func (s *inputStates) apply(u *ssp.Update) ([]wire.UserInstruction, error) {
	pos, ok := s.ends[u.OldNum]
	if !ok {
		return nil, fmt.Errorf("missing input base %d", u.OldNum)
	}
	// HostBytes and Keystroke use the same protobuf field numbers; upstream
	// exports only the host decoder. Convert immediately to typed user actions.
	messages, err := wire.UnmarshalHostMessage(u.Diff)
	if err != nil {
		return nil, err
	}
	var result []wire.UserInstruction
	for _, m := range messages {
		if m.Width != 0 || m.Height != 0 {
			pos++
			if pos > s.delivered {
				result = append(result, wire.UserInstruction{Width: m.Width, Height: m.Height})
			}
		}
		if len(m.Hoststring) > 0 {
			end := pos + uint64(len(m.Hoststring))
			skip := uint64(0)
			if s.delivered > pos {
				skip = min(s.delivered-pos, uint64(len(m.Hoststring)))
			}
			if skip < uint64(len(m.Hoststring)) {
				result = append(result, wire.UserInstruction{Keys: m.Hoststring[skip:]})
			}
			pos = end
		}
	}
	s.ends[u.NewNum] = pos
	if pos > s.delivered {
		s.delivered = pos
	}
	for n := range s.ends {
		if n < u.ThrowawayNum {
			delete(s.ends, n)
		}
	}
	return result, nil
}
