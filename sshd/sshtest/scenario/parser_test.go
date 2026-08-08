package scenario

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseSimpleScenario(t *testing.T) {
	input := `
name: test scenario
description: A test

steps:
  - client: alice
    actions:
      - connect
      - exec: "echo hello"
    expect:
      - output: "hello"
`
	scenario, err := Parse(input)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if scenario.Name != "test scenario" {
		t.Errorf("wrong name: %s", scenario.Name)
	}
	if len(scenario.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(scenario.Steps))
	}
	if scenario.Steps[0].Client != "alice" {
		t.Errorf("wrong client: %s", scenario.Steps[0].Client)
	}
	if len(scenario.Steps[0].Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(scenario.Steps[0].Actions))
	}
}

func TestParseActionSpecificScalarAndSFTPParams(t *testing.T) {
	input := `
name: typed params
steps:
  - actions:
      - exec: echo command
      - input: typed input
      - line: typed line
      - key: Enter
      - sleep: 25ms
      - wait_for_event: event.name
      - sftp_upload: {local: local-a, remote: remote-a}
      - sftp_download: {remote: remote-b, local: local-b}
    expect:
      - output_match: pattern.*
      - event: event.name
      - exit_code: 42
      - wait_for_output: {text: ready, timeout: 2s}
`
	sc, err := Parse(input)
	if err != nil {
		t.Fatalf("parse typed params: %v", err)
	}
	actions := sc.Steps[0].Actions
	if actions[0].Command() != "echo command" || actions[1].Text() != "typed input" || actions[2].Text() != "typed line" {
		t.Fatalf("scalar action params = %#v", actions[:3])
	}
	if actions[3].KeyName() != "Enter" || actions[4].Duration() != 25*time.Millisecond || actions[5].EventID() != "event.name" {
		t.Fatalf("typed action params = %#v", actions[3:6])
	}
	if actions[6].Type != ActionSFTPUpload || actions[6].LocalPath() != "local-a" || actions[6].RemotePath() != "remote-a" {
		t.Fatalf("SFTP upload params = %#v", actions[6])
	}
	if actions[7].Type != ActionSFTPDownload || actions[7].LocalPath() != "local-b" || actions[7].RemotePath() != "remote-b" {
		t.Fatalf("SFTP download params = %#v", actions[7])
	}
	expects := sc.Steps[0].Expect
	if expects[0].Pattern() != "pattern.*" || expects[1].EventID() != "event.name" || expects[2].Code() != 42 {
		t.Fatalf("expectation params = %#v", expects[:3])
	}
	if expects[3].Text() != "ready" || expects[3].Timeout() != 2*time.Second {
		t.Fatalf("wait expectation params = %#v", expects[3])
	}
}

func TestParseDurationNumericMilliseconds(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    time.Duration
		wantErr string
	}{
		{name: "normal integer", value: 2, want: 2 * time.Millisecond},
		{name: "normal float", value: 1.5, want: 1500 * time.Microsecond},
		{name: "fractional millisecond", value: 0.5, want: 500 * time.Microsecond},
		{name: "tiny underflow", value: 0.0000001, wantErr: "underflow"},
		{name: "not a number", value: math.NaN(), wantErr: "finite"},
		{name: "positive infinity", value: math.Inf(1), wantErr: "finite"},
		{name: "negative infinity", value: math.Inf(-1), wantErr: "finite"},
		{name: "overflow", value: float64(math.MaxInt64) / float64(time.Millisecond), wantErr: "overflow"},
		{name: "zero integer", value: 0, wantErr: "positive"},
		{name: "zero float", value: 0.0, wantErr: "positive"},
		{name: "negative integer", value: -1, wantErr: "positive"},
		{name: "negative string", value: "-1ms", wantErr: "positive"},
		{name: "zero string", value: "0s", wantErr: "positive"},
		{name: "normal string", value: "250us", want: 250 * time.Microsecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDuration(test.value)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseDuration(%v) error = %v, want containing %q", test.value, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("parseDuration(%v) = %v, %v; want %v, nil", test.value, got, err, test.want)
			}
		})
	}
}

func TestParseNumericDurationsForSleepAndTimeout(t *testing.T) {
	input := `
steps:
  - actions:
      - sleep: 0.5
      - wait_for_event: {event: ready, timeout: 1.5}
    expect:
      - wait_for_output: {text: ready, timeout: 2}
`
	sc, err := Parse(input)
	if err != nil {
		t.Fatalf("parse numeric durations: %v", err)
	}
	if got := sc.Steps[0].Actions[0].Duration(); got != 500*time.Microsecond {
		t.Fatalf("fractional sleep = %v, want 500us", got)
	}
	if got := sc.Steps[0].Actions[1].Timeout(); got != 1500*time.Microsecond {
		t.Fatalf("fractional action timeout = %v, want 1.5ms", got)
	}
	if got := sc.Steps[0].Expect[0].Timeout(); got != 2*time.Millisecond {
		t.Fatalf("integer expectation timeout = %v, want 2ms", got)
	}
}

func TestParseRejectsInvalidSleepAndTimeoutDurations(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "zero sleep", body: "actions: [{sleep: 0}]", wantErr: "positive"},
		{name: "negative sleep", body: "actions: [{sleep: -1}]", wantErr: "positive"},
		{name: "sleep underflow", body: "actions: [{sleep: 0.0000001}]", wantErr: "underflow"},
		{name: "sleep NaN", body: "actions: [{sleep: .nan}]", wantErr: "finite"},
		{name: "action timeout infinity", body: "actions: [{wait_for_event: {event: ready, timeout: .inf}}]", wantErr: "finite"},
		{name: "expectation timeout overflow", body: "expect: [{wait_for_output: {text: ready, timeout: 1e20}}]", wantErr: "overflow"},
		{name: "zero string timeout", body: "expect: [{wait_for_output: {text: ready, timeout: 0s}}]", wantErr: "positive"},
		{name: "negative string timeout", body: "actions: [{wait_for_event: {event: ready, timeout: -1ms}}]", wantErr: "positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := "steps:\n  - " + test.body + "\n"
			_, err := Parse(input)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("parse error = %v, want containing %q\n%s", err, test.wantErr, input)
			}
		})
	}
}

func TestParseRejectsIncompleteSFTPMap(t *testing.T) {
	_, err := Parse("name: invalid\nsteps:\n  - actions:\n      - sftp_upload: {local: only}\n")
	if err == nil || !strings.Contains(err.Error(), `requires field "remote"`) {
		t.Fatalf("incomplete SFTP parse error = %v", err)
	}
}

func TestParseValidActionAndExpectationForms(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"connect bare", "actions: [connect]"},
		{"disconnect null map", "actions: [{disconnect: null}]"},
		{"shell bare", "actions: [shell]"},
		{"close shell empty map", "actions: [{close_shell: {}}]"},
		{"exec shorthand", "actions: [{exec: echo ok}]"},
		{"exec map", "actions: [{exec: {command: echo ok}}]"},
		{"input shorthand", "actions: [{input: input}]"},
		{"line map", "actions: [{line: {text: line}}]"},
		{"key shorthand", "actions: [{key: Enter}]"},
		{"sleep shorthand", "actions: [{sleep: 1ms}]"},
		{"sleep numeric shorthand", "actions: [{sleep: 10}]"},
		{"sleep map", "actions: [{sleep: {duration: 2ms}}]"},
		{"resize map", "actions: [{resize: {cols: 80, rows: 24}}]"},
		{"wait event shorthand", "actions: [{wait_for_event: ready}]"},
		{"wait event map", "actions: [{wait_for_event: {event: ready, attrs: [key, value], timeout: 1s}}]"},
		{"local forward", "actions: [{local_forward: {local: '127.0.0.1:0', remote: '127.0.0.1:1'}}]"},
		{"remote forward", "actions: [{remote_forward: {remote: '127.0.0.1:0', local: '127.0.0.1:1'}}]"},
		{"sftp upload", "actions: [{sftp_upload: {local: local, remote: remote}}]"},
		{"sftp download", "actions: [{sftp_download: {remote: remote, local: local}}]"},
		{"connected bare", "expect: [connected]"},
		{"disconnected null", "expect: [{disconnected: null}]"},
		{"output shorthand", "expect: [{output: value}]"},
		{"output map", "expect: [{output: {contains: value}}]"},
		{"output match shorthand", "expect: [{output_match: 'v.*'}]"},
		{"stdout map", "expect: [{stdout: {contains: value}}]"},
		{"stderr shorthand", "expect: [{stderr: value}]"},
		{"exit shorthand", "expect: [{exit_code: 42}]"},
		{"exit map", "expect: [{exit_code: {code: 42}}]"},
		{"screen shorthand", "expect: [{screen: value}]"},
		{"event shorthand", "expect: [{event: ready}]"},
		{"event map", "expect: [{event: {event: ready, attrs: [key, value]}}]"},
		{"no event shorthand", "expect: [{no_event: ready}]"},
		{"wait output shorthand", "expect: [{wait_for_output: ready}]"},
		{"wait output map", "expect: [{wait_for_output: {text: ready, timeout: 1s}}]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := "name: valid\nsteps:\n  - " + test.yaml + "\n"
			if _, err := Parse(input); err != nil {
				t.Fatalf("parse valid form: %v\n%s", err, input)
			}
		})
	}
}

func TestParseRejectsInvalidActionAndExpectationForms(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"unknown action", "actions: [future_action]", "unknown action type"},
		{"multi action mapping", "actions: [{exec: ok, sleep: 1s}]", "exactly one action"},
		{"parameterless with value", "actions: [{connect: value}]", "does not accept parameters"},
		{"exec null", "actions: [{exec: null}]", "must be a string or mapping"},
		{"exec wrong type", "actions: [{exec: 7}]", "got int"},
		{"exec unknown field", "actions: [{exec: {command: ok, extra: bad}}]", "unknown field"},
		{"resize missing", "actions: [{resize: {cols: 80}}]", "requires field \"rows\""},
		{"resize wrong", "actions: [{resize: {cols: wide, rows: 24}}]", "positive integer"},
		{"wait attrs wrong", "actions: [{wait_for_event: {event: ready, attrs: bad}}]", "invalid string list"},
		{"wait attrs odd", "actions: [{wait_for_event: {event: ready, attrs: [key]}}]", "key/value pairs"},
		{"forward empty", "actions: [{local_forward: {local: '', remote: remote}}]", "must not be empty"},
		{"forward wrong", "actions: [{remote_forward: {local: 7, remote: remote}}]", "must be a string"},
		{"sftp bare", "actions: [sftp_upload]", "requires parameters"},
		{"sftp scalar", "actions: [{sftp_upload: local}]", "requires a mapping"},
		{"sftp null", "actions: [{sftp_download: null}]", "must be a string or mapping"},
		{"sftp empty local", "actions: [{sftp_upload: {local: '', remote: remote}}]", "must not be empty"},
		{"sftp empty remote", "actions: [{sftp_download: {remote: '', local: local}}]", "must not be empty"},
		{"sftp wrong path", "actions: [{sftp_upload: {local: 7, remote: remote}}]", "must be a string"},
		{"sftp unknown field", "actions: [{sftp_upload: {local: local, remote: remote, mode: 7}}]", "unknown field"},
		{"unknown expectation", "expect: [future_expect]", "unknown expectation type"},
		{"multi expectation mapping", "expect: [{output: ok, stderr: bad}]", "exactly one expectation"},
		{"connected with value", "expect: [{connected: yes}]", "does not accept parameters"},
		{"output wrong type", "expect: [{output: 7}]", "invalid type int"},
		{"output missing", "expect: [{stdout: {}}]", "requires field \"contains\""},
		{"output unknown", "expect: [{stderr: {contains: bad, extra: bad}}]", "unknown field"},
		{"pattern wrong", "expect: [{output_match: {pattern: 7}}]", "must be a string"},
		{"exit wrong", "expect: [{exit_code: nope}]", "requires an integer"},
		{"exit fractional", "expect: [{exit_code: 1.5}]", "invalid type float64"},
		{"event wrong", "expect: [{event: {event: 7}}]", "must be a string"},
		{"event attrs odd", "expect: [{event: {event: ready, attrs: [key]}}]", "key/value pairs"},
		{"wait output null", "expect: [{wait_for_output: null}]", "invalid type"},
		{"wait output unknown", "expect: [{wait_for_output: {text: ok, extra: bad}}]", "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := "name: invalid\nsteps:\n  - " + test.body + "\n"
			_, err := Parse(input)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("parse error = %v, want containing %q\n%s", err, test.wantErr, input)
			}
		})
	}
}

func TestParseAllActionTypes(t *testing.T) {
	input := `
name: all actions
steps:
  - client: test
    actions:
      - connect
      - disconnect
      - shell
      - close_shell
      - exec: "command"
      - input: "text"
      - line: "line text"
      - key: Enter
      - sleep: 100ms
      - resize:
          cols: 80
          rows: 24
`
	scenario, err := Parse(input)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(scenario.Steps[0].Actions) != 10 {
		t.Errorf("expected 10 actions, got %d", len(scenario.Steps[0].Actions))
	}

	// Verify action types
	expected := []ActionType{
		ActionConnect,
		ActionDisconnect,
		ActionShell,
		ActionCloseShell,
		ActionExec,
		ActionInput,
		ActionLine,
		ActionKey,
		ActionSleep,
		ActionResize,
	}

	for i, exp := range expected {
		if scenario.Steps[0].Actions[i].Type != exp {
			t.Errorf("action %d: expected %s, got %s", i, exp, scenario.Steps[0].Actions[i].Type)
		}
	}
}

func TestParseAllExpectationTypes(t *testing.T) {
	input := `
name: all expectations
steps:
  - client: test
    expect:
      - connected
      - disconnected
      - output: "text"
      - output_match: "pattern.*"
      - stdout: "out"
      - stderr: "err"
      - exit_code: 0
      - screen: "screen text"
      - event: "test.event"
`
	scenario, err := Parse(input)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(scenario.Steps[0].Expect) != 9 {
		t.Errorf("expected 9 expectations, got %d", len(scenario.Steps[0].Expect))
	}

	expected := []ExpectationType{
		ExpectConnected,
		ExpectDisconnected,
		ExpectOutput,
		ExpectOutputMatch,
		ExpectStdout,
		ExpectStderr,
		ExpectExitCode,
		ExpectScreen,
		ExpectEvent,
	}

	for i, exp := range expected {
		if scenario.Steps[0].Expect[i].Type != exp {
			t.Errorf("expectation %d: expected %s, got %s", i, exp, scenario.Steps[0].Expect[i].Type)
		}
	}
}

func TestLoadFixture(t *testing.T) {
	scenario, err := LoadFixture("echo")
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	if scenario.Name != "echo command" {
		t.Errorf("wrong name: %s", scenario.Name)
	}
}

func TestListFixtures(t *testing.T) {
	fixtures, err := ListFixtures()
	if err != nil {
		t.Fatalf("failed to list fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Error("expected at least one fixture")
	}

	// Check that echo fixture is in the list
	found := false
	for _, name := range fixtures {
		if name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("echo fixture not found in %v", fixtures)
	}
}

func TestLoadAllFixtures(t *testing.T) {
	scenarios, err := LoadAllFixtures()
	if err != nil {
		t.Fatalf("failed to load all fixtures: %v", err)
	}

	if len(scenarios) == 0 {
		t.Error("expected at least one scenario")
	}

	// All should have names
	for _, s := range scenarios {
		if s.Name == "" {
			t.Error("scenario has empty name")
		}
	}
}

func TestActionHelpers(t *testing.T) {
	input := `
name: helper test
steps:
  - client: test
    actions:
      - exec: "my command"
      - resize:
          cols: 120
          rows: 40
`
	scenario, err := Parse(input)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	execAction := scenario.Steps[0].Actions[0]
	if execAction.Command() != "my command" {
		t.Errorf("wrong command: %s", execAction.Command())
	}

	resizeAction := scenario.Steps[0].Actions[1]
	if resizeAction.Cols() != 120 {
		t.Errorf("wrong cols: %d", resizeAction.Cols())
	}
	if resizeAction.Rows() != 40 {
		t.Errorf("wrong rows: %d", resizeAction.Rows())
	}
}
