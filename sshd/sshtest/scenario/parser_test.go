package scenario

import (
	"testing"
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
