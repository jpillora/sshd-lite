package sshtest_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/key"
	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/sshtest/log"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"github.com/jpillora/sshd-lite/sshd/xnet"
)

func TestKeyFromSeed(t *testing.T) {
	// Same seed should produce same key
	key1, err := key.SignerFromSeed("test-seed")
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	key2, err := key.SignerFromSeed("test-seed")
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Compare public keys
	pub1 := key1.PublicKey().Marshal()
	pub2 := key2.PublicKey().Marshal()

	if string(pub1) != string(pub2) {
		t.Error("same seed should produce same key")
	}

	// Different seed should produce different key
	key3, err := key.SignerFromSeed("different-seed")
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	pub3 := key3.PublicKey().Marshal()
	if string(pub1) == string(pub3) {
		t.Error("different seeds should produce different keys")
	}
}

func TestEventBus(t *testing.T) {
	bus := sshtest.NewEventBus()

	// Test emit and find
	bus.Emit("test.event", "key1", "value1", "key2", "value2")

	event, found := bus.Find("test.event")
	if !found {
		t.Fatal("event not found")
	}
	if event.Attrs["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %s", event.Attrs["key1"])
	}

	// Test find with attributes
	event, found = bus.Find("test.event", "key1", "value1")
	if !found {
		t.Fatal("event with matching attrs not found")
	}

	event, found = bus.Find("test.event", "key1", "wrong")
	if found {
		t.Fatal("should not find event with wrong attrs")
	}

	// Test Has
	if !bus.Has("test.event") {
		t.Error("Has should return true for existing event")
	}
	if bus.Has("nonexistent") {
		t.Error("Has should return false for nonexistent event")
	}

	// Test Clear
	bus.Clear()
	if bus.Count() != 0 {
		t.Error("Clear should remove all events")
	}
}

func TestEventBusWait(t *testing.T) {
	bus := sshtest.NewEventBus()

	// Emit event in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		bus.Emit("delayed.event", "status", "ok")
	}()

	// Wait for event
	event, err := bus.WaitTimeout(time.Second, "delayed.event")
	if err != nil {
		t.Fatalf("WaitTimeout failed: %v", err)
	}
	if event.Attrs["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", event.Attrs["status"])
	}

	// Test timeout
	_, err = bus.WaitTimeout(50*time.Millisecond, "nonexistent")
	if err == nil {
		t.Error("should timeout waiting for nonexistent event")
	}
}

func TestLogCapture(t *testing.T) {
	lc := log.NewCapture()
	logger := lc.Logger()

	logger.Info("test info message")
	logger.Error("test error message")
	logger.Debug("test debug message")

	if lc.Count() != 3 {
		t.Errorf("expected 3 entries, got %d", lc.Count())
	}

	if err := lc.Assert("info message"); err != nil {
		t.Errorf("Assert failed: %v", err)
	}

	if err := lc.Assert("nonexistent"); err == nil {
		t.Error("Assert should fail for nonexistent message")
	}
}

func TestFindFreePort(t *testing.T) {
	port, err := xnet.FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort failed: %v", err)
	}
	if port <= 0 {
		t.Errorf("invalid port: %d", port)
	}
}

func TestEnvironmentBasic(t *testing.T) {
	env := sshtest.New(t).
		WithServer(sshtest.ServerWithPassword("user", "pass")).
		WithClient("test", sshtest.ClientWithPassword("pass")).
		Start()
	defer env.Stop()

	// Check server is running
	if env.Server() == nil {
		t.Fatal("server should be running")
	}
	if env.Server().Port() <= 0 {
		t.Error("server should have a valid port")
	}

	// Check client exists
	client := env.Client("test")
	if client == nil {
		t.Fatal("client should exist")
	}

	// Connect and exec
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	result, err := client.Exec("echo hello")
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("expected 'hello' in stdout, got: %s", result.Stdout)
	}

	// Check events
	if !env.Events().Has(scenario.EventConnected) {
		t.Error("should have connected event")
	}
	if !env.Events().Has(scenario.EventExecCompleted) {
		t.Error("should have exec completed event")
	}
}

func TestEnvironmentWithKeySeed(t *testing.T) {
	env := sshtest.New(t).
		WithServer().
		WithClient("alice", sshtest.ClientWithKeySeed("alice")).
		Start()
	defer env.Stop()

	client := env.Client("alice")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect with key: %v", err)
	}

	result, err := client.Exec("echo key-auth-works")
	if err != nil {
		t.Fatalf("failed to exec: %v", err)
	}

	if !strings.Contains(result.Stdout, "key-auth-works") {
		t.Errorf("expected output, got: %s", result.Stdout)
	}
}

func TestScenarioBuilder(t *testing.T) {
	scenario := sshtest.NewScenario("test scenario").
		Description("A test scenario").
		Step("alice").
		Do(scenario.ConnectSpec()).
		Do(scenario.ExecSpec("echo hello")).
		Then(scenario.OutputSpec("hello")).
		Then(scenario.ExitCodeSpec(0)).
		Build()

	if scenario.Name != "test scenario" {
		t.Errorf("wrong name: %s", scenario.Name)
	}
	if len(scenario.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(scenario.Steps))
	}
	if len(scenario.Steps[0].Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(scenario.Steps[0].Actions))
	}
	if len(scenario.Steps[0].Expect) != 2 {
		t.Errorf("expected 2 expectations, got %d", len(scenario.Steps[0].Expect))
	}
}

func TestScenarioExecution(t *testing.T) {
	env := sshtest.New(t).
		WithServer(sshtest.ServerWithPassword("user", "pass")).
		WithClient("test", sshtest.ClientWithPassword("pass")).
		Start()
	defer env.Stop()

	scenario := sshtest.NewScenario("exec test").
		Step("test").
		Do(scenario.ConnectSpec()).
		Do(scenario.ExecSpec("echo scenario-works")).
		Then(scenario.OutputSpec("scenario-works")).
		Then(scenario.ExitCodeSpec(0)).
		Then(scenario.EventSpec(scenario.EventExecCompleted)).
		Build()

	err := env.Run(scenario)
	if err != nil {
		t.Fatalf("scenario failed: %v", err)
	}
}

func TestYAMLParsing(t *testing.T) {
	yaml := `
name: YAML test
description: Test YAML parsing

steps:
  - client: alice
    actions:
      - connect
      - exec: "echo hello"
      - sleep: 100ms
    expect:
      - output: "hello"
      - exit_code: 0
      - event: exec.completed
`

	sc, err := scenario.Parse(yaml)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if sc.Name != "YAML test" {
		t.Errorf("wrong name: %s", sc.Name)
	}
	if len(sc.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(sc.Steps))
	}
	if sc.Steps[0].Client != "alice" {
		t.Errorf("wrong client: %s", sc.Steps[0].Client)
	}
	if len(sc.Steps[0].Actions) != 3 {
		t.Errorf("expected 3 actions, got %d", len(sc.Steps[0].Actions))
	}
	if len(sc.Steps[0].Expect) != 3 {
		t.Errorf("expected 3 expectations, got %d", len(sc.Steps[0].Expect))
	}
}

func TestYAMLScenarioExecution(t *testing.T) {
	env := sshtest.New(t).
		WithServer(sshtest.ServerWithPassword("user", "pass")).
		WithClient("test", sshtest.ClientWithPassword("pass")).
		Start()
	defer env.Stop()

	yaml := `
name: YAML exec test
steps:
  - client: test
    actions:
      - connect
      - exec: "echo yaml-works"
    expect:
      - output: "yaml-works"
`

	err := env.RunYAML(yaml)
	if err != nil {
		t.Fatalf("YAML scenario failed: %v", err)
	}
}

func TestMultipleClients(t *testing.T) {
	env := sshtest.New(t).
		WithServer().
		WithClient("alice", sshtest.ClientWithKeySeed("alice")).
		WithClient("bob", sshtest.ClientWithKeySeed("bob")).
		Start()
	defer env.Stop()

	// Both clients should be able to connect
	alice := env.Client("alice")
	bob := env.Client("bob")

	if err := alice.Connect(); err != nil {
		t.Fatalf("alice failed to connect: %v", err)
	}
	if err := bob.Connect(); err != nil {
		t.Fatalf("bob failed to connect: %v", err)
	}

	// Both should be able to exec
	resultAlice, err := alice.Exec("echo alice")
	if err != nil {
		t.Fatalf("alice exec failed: %v", err)
	}
	resultBob, err := bob.Exec("echo bob")
	if err != nil {
		t.Fatalf("bob exec failed: %v", err)
	}

	if !strings.Contains(resultAlice.Stdout, "alice") {
		t.Error("alice output wrong")
	}
	if !strings.Contains(resultBob.Stdout, "bob") {
		t.Error("bob output wrong")
	}
}

func TestShellSession(t *testing.T) {
	env := sshtest.New(t).
		WithServer().
		WithClient("test", sshtest.ClientWithKeySeed("test"), sshtest.ClientWithPTY(80, 24)).
		Start()
	defer env.Stop()

	client := env.Client("test")
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	sess, err := client.Shell()
	if err != nil {
		t.Fatalf("failed to start shell: %v", err)
	}
	defer sess.Close()

	// Send a command
	_, err = sess.Write([]byte("echo shell-test\r"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Wait for output
	err = sess.WaitForOutput("shell-test", 5*time.Second)
	if err != nil {
		t.Fatalf("failed to wait for output: %v", err)
	}

	// Check events
	if !env.Events().Has(scenario.EventShellStarted) {
		t.Error("should have shell started event")
	}
}

func TestActions(t *testing.T) {
	// Test action string representations
	tests := []struct {
		action   scenario.Action
		expected string
	}{
		{sshtest.Connect(), "Connect"},
		{sshtest.Disconnect(), "Disconnect"},
		{sshtest.Exec("echo test"), `Exec("echo test")`},
		{sshtest.StartShell(), "StartShell"},
		{sshtest.CloseShell(), "CloseShell"},
		{sshtest.SendInput("hello"), `SendInput("hello")`},
		{sshtest.SendLine("hello"), `SendLine("hello")`},
		{sshtest.SendKey(scenario.KeyEnter), "SendKey(Enter)"},
		{sshtest.SendKey(scenario.KeyCtrlC), "SendKey(Ctrl+C)"},
		{sshtest.Sleep(100 * time.Millisecond), "Sleep(100ms)"},
		{sshtest.ResizePTY(80, 24), "ResizePTY(80, 24)"},
	}

	for _, tc := range tests {
		if tc.action.String() != tc.expected {
			t.Errorf("expected %q, got %q", tc.expected, tc.action.String())
		}
	}
}

func TestExpectations(t *testing.T) {
	// Test expectation string representations
	tests := []struct {
		expect   scenario.Expectation
		expected string
	}{
		{sshtest.ExpectOutput("hello"), `ExpectOutput("hello")`},
		{sshtest.ExpectOutputMatch("hel.*"), `ExpectOutputMatch("hel.*")`},
		{sshtest.ExpectExitCode(0), "ExpectExitCode(0)"},
		{sshtest.ExpectConnected(), "ExpectConnected"},
		{sshtest.ExpectDisconnected(), "ExpectDisconnected"},
		{sshtest.ExpectEvent("test.event"), `ExpectEvent("test.event")`},
		{sshtest.ExpectNoEvent("test.event"), `ExpectNoEvent("test.event")`},
	}

	for _, tc := range tests {
		if tc.expect.String() != tc.expected {
			t.Errorf("expected %q, got %q", tc.expected, tc.expect.String())
		}
	}
}

func TestYAMLKeyParsing(t *testing.T) {
	yaml := `
name: key test
steps:
  - client: test
    actions:
      - key: Enter
      - key: Tab
      - key: Ctrl+C
      - key: Up
`

	sc, err := scenario.Parse(yaml)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if len(sc.Steps[0].Actions) != 4 {
		t.Errorf("expected 4 actions, got %d", len(sc.Steps[0].Actions))
	}
}

func TestYAMLResizeParsing(t *testing.T) {
	yaml := `
name: resize test
steps:
  - client: test
    actions:
      - resize:
          cols: 120
          rows: 40
`

	sc, err := scenario.Parse(yaml)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if len(sc.Steps[0].Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(sc.Steps[0].Actions))
	}

	action := sc.Steps[0].Actions[0]
	if action.Type != "resize" {
		t.Errorf("wrong action type: %s", action.Type)
	}
	if action.Cols() != 120 {
		t.Errorf("wrong cols: %d", action.Cols())
	}
	if action.Rows() != 40 {
		t.Errorf("wrong rows: %d", action.Rows())
	}
}

func TestExecScenarioFromYAML(t *testing.T) {
	// Load the exec scenario from YAML file
	yamlData, err := os.ReadFile("testdata/exec.yaml")
	if err != nil {
		t.Fatalf("failed to read scenario file: %v", err)
	}

	env := sshtest.New(t).
		WithServer(sshtest.ServerWithNoAuth()).
		WithClient("test", sshtest.ClientWithNoAuth()).
		Start()
	defer env.Stop()

	err = env.RunYAML(string(yamlData))
	if err != nil {
		t.Fatalf("scenario failed: %v", err)
	}
}

func TestRequireHelpers(t *testing.T) {
	// Test the Require helper (using a mock T)
	mt := &mockT{}
	env := &sshtest.Environment{}
	// Access Require through reflection since env isn't properly initialized
	r := &sshtest.Require{}
	_ = r // Just verify it compiles

	// These would fail in a real test, just checking they exist
	_ = mt
	_ = env
}

type mockT struct {
	testing.TB
	failed bool
}

func (m *mockT) Helper()                                   {}
func (m *mockT) Fatalf(format string, args ...interface{}) { m.failed = true }
func (m *mockT) Fatal(args ...interface{})                 { m.failed = true }
func (m *mockT) Logf(format string, args ...interface{})   {}
