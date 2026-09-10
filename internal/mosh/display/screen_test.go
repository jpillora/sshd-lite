package display

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jpillora/sshd-lite/internal/mosh/ssp"
	wire "github.com/unixshells/mosh-go"
)

func TestReceiverInitialRenderDoesNotClearOrSwitchScreens(t *testing.T) {
	s := NewReceiver(20, 4)
	defer s.Close()
	out, err := s.Apply(&ssp.Update{
		OldNum: 0,
		NewNum: 1,
		Diff: wire.MarshalHostMessage([]wire.HostInstruction{{
			Hoststring: []byte("prompt"),
			EchoAckNum: -1,
		}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\x1b[2J", "\x1b[?1049h", "\x1b[?1049l"} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("initial render contains screen-changing sequence %q: %q", forbidden, out)
		}
	}
	if !strings.Contains(string(out), "prompt") {
		t.Fatalf("initial render omitted remote contents: %q", out)
	}
}

func TestReceiverPropagatesApplicationAlternateScreen(t *testing.T) {
	s := NewReceiver(20, 4)
	defer s.Close()
	apply := func(old, next uint64, text string) string {
		t.Helper()
		out, err := s.Apply(&ssp.Update{
			OldNum: old,
			NewNum: next,
			Diff: wire.MarshalHostMessage([]wire.HostInstruction{{
				Hoststring: []byte(text),
				EchoAckNum: -1,
			}}),
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	_ = apply(0, 1, "PRIMARY")
	entered := apply(1, 2, "\x1b[?1049hALT")
	if i, j := strings.Index(entered, "\x1b[?1049h"), strings.LastIndex(entered, "ALT"); i < 0 || j < 0 || i > j {
		t.Fatalf("alternate screen was not selected before rendering: %q", entered)
	}
	if _, err := s.Apply(&ssp.Update{
		OldNum: 2,
		NewNum: 3,
		Diff: wire.MarshalHostMessage([]wire.HostInstruction{{
			Width:      30,
			Height:     6,
			EchoAckNum: -1,
		}}),
	}); err != nil {
		t.Fatal(err)
	}
	left := apply(3, 4, "\x1b[?1049lAFTER")
	if !strings.Contains(left, "\x1b[?1049l") {
		t.Fatalf("alternate screen restoration did not leave alternate mode: %q", left)
	}
	for _, overwritten := range []string{"PRIMARY", "AFTER", "\x1b[1;"} {
		if strings.Contains(left, overwritten) {
			t.Fatalf("alternate screen restoration painted %q over local primary: %q", overwritten, left)
		}
	}
}

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
