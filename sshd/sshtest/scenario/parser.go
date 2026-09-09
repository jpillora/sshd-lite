package scenario

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func Parse(input string) (*Scenario, error) {
	var sc Scenario
	if err := yaml.Unmarshal([]byte(input), &sc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return &sc, nil
}

type rawAction struct {
	Key   string
	Value interface{}
}

func (r *rawAction) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("action must be a mapping, got %v", node.Kind)
	}
	if len(node.Content) != 2 {
		return fmt.Errorf("action mapping must contain exactly one action, got %d", len(node.Content)/2)
	}
	var key string
	if err := node.Content[0].Decode(&key); err != nil {
		return err
	}
	r.Key = strings.ToLower(key)
	return node.Content[1].Decode(&r.Value)
}

type rawExpectation struct {
	Key   string
	Value interface{}
}

func (r *rawExpectation) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expectation must be a mapping, got %v", node.Kind)
	}
	if len(node.Content) != 2 {
		return fmt.Errorf("expectation mapping must contain exactly one expectation, got %d", len(node.Content)/2)
	}
	var key string
	if err := node.Content[0].Decode(&key); err != nil {
		return err
	}
	r.Key = strings.ToLower(key)
	return node.Content[1].Decode(&r.Value)
}

func (a *ActionSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var name string
		if err := node.Decode(&name); err != nil {
			return err
		}
		a.Type = ActionType(strings.ToLower(name))
		if !isKnownAction(a.Type) {
			return fmt.Errorf("unknown action type %q", a.Type)
		}
		if !isParameterlessAction(a.Type) {
			return fmt.Errorf("action %q requires parameters", a.Type)
		}
		a.Params = map[string]interface{}{}
		return nil
	case yaml.MappingNode:
		var raw rawAction
		if err := node.Decode(&raw); err != nil {
			return err
		}
		a.Type = ActionType(raw.Key)
		if !isKnownAction(a.Type) {
			return fmt.Errorf("unknown action type %q", a.Type)
		}
		params, err := parseActionParams(a.Type, raw.Value)
		if err != nil {
			return err
		}
		a.Params = params
		return nil
	default:
		return fmt.Errorf("invalid action format: %v", node.Kind)
	}
}

func (e *ExpectationSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var name string
		if err := node.Decode(&name); err != nil {
			return err
		}
		e.Type = ExpectationType(strings.ToLower(name))
		if !isKnownExpectation(e.Type) {
			return fmt.Errorf("unknown expectation type %q", e.Type)
		}
		if e.Type != ExpectConnected && e.Type != ExpectDisconnected {
			return fmt.Errorf("expectation %q requires parameters", e.Type)
		}
		e.Params = map[string]interface{}{}
		return nil
	case yaml.MappingNode:
		var raw rawExpectation
		if err := node.Decode(&raw); err != nil {
			return err
		}
		e.Type = ExpectationType(raw.Key)
		if !isKnownExpectation(e.Type) {
			return fmt.Errorf("unknown expectation type %q", e.Type)
		}
		params, err := parseExpectationParams(e.Type, raw.Value)
		if err != nil {
			return err
		}
		e.Params = params
		return nil
	default:
		return fmt.Errorf("invalid expectation format: %v", node.Kind)
	}
}

func isKnownAction(actionType ActionType) bool {
	switch actionType {
	case ActionConnect, ActionDisconnect, ActionShell, ActionCloseShell,
		ActionExec, ActionInput, ActionLine, ActionKey, ActionSleep, ActionResize,
		ActionWaitForEvent, ActionLocalForward, ActionRemoteForward,
		ActionSFTPUpload, ActionSFTPDownload:
		return true
	default:
		return false
	}
}

func isParameterlessAction(actionType ActionType) bool {
	return actionType == ActionConnect || actionType == ActionDisconnect || actionType == ActionShell || actionType == ActionCloseShell
}

func isKnownExpectation(expectationType ExpectationType) bool {
	switch expectationType {
	case ExpectConnected, ExpectDisconnected, ExpectOutput, ExpectOutputMatch,
		ExpectStdout, ExpectStderr, ExpectExitCode, ExpectScreen, ExpectEvent,
		ExpectNoEvent, ExpectWaitForOutput:
		return true
	default:
		return false
	}
}
