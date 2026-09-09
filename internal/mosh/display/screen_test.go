package display

import (
	"reflect"
	"testing"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

func TestScreenOldBaseAndUnicode(t *testing.T) {
	s := NewReceiver(20, 4)
	defer s.scratch.Close()
	apply := func(old, new uint64, text string) {
		t.Helper()
		_, err := s.Apply(&ssp.Update{OldNum: old, NewNum: new, Diff: wire.MarshalHostMessage([]wire.HostInstruction{{Hoststring: []byte(text), EchoAckNum: -1}})})
		if err != nil {
			t.Fatal(err)
		}
	}
	apply(0, 1, "\x1b[31mA界e\u0301\x1b[?25l\x1b[?2004h")
	apply(1, 2, "\x1b[2;1HOLD")
	apply(1, 3, "\x1b[3;1HNEW")
	state := s.states[3]
	if state.cells[20].Content != " " || state.cells[40].Content != "N" {
		t.Fatal("Diff applied to latest Screen instead of named base")
	}
	scratch := New(20, 4)
	defer scratch.Close()
	scratch.CloseResponses()
	scratch.Restore(state)
	restored := scratch.Snapshot()
	if !reflect.DeepEqual(state, restored) {
		t.Fatalf("Screen roundtrip differs: original=%+v restored=%+v", state, restored)
	}
	if state.cells[3].Content != "e\u0301" {
		t.Fatalf("combining mark lost: %+v", state.cells[3])
	}
}
func TestCursorKeysAcrossInputPackets(t *testing.T) {
	for _, application := range []bool{false, true} {
		var keys CursorKeys
		input := "a\x1bOA\x1bOB\x1bOC\x1bOD\x1bOP\x1b[1;5A\x1b"
		var output []byte
		for i := range len(input) {
			output = append(output, keys.Translate([]byte{input[i]}, application)...)
		}
		want := input
		if !application {
			want = "a\x1b[A\x1b[B\x1b[C\x1b[D\x1bOP\x1b[1;5A\x1b"
		}
		if string(output) != want {
			t.Fatalf("application=%v got=%q want=%q", application, output, want)
		}
	}
}
