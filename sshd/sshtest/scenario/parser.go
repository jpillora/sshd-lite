package scenario

import (
	"fmt"
	"strings"
	"time"

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
	if len(node.Content) < 2 {
		return fmt.Errorf("action mapping has no content")
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
	if len(node.Content) < 2 {
		return fmt.Errorf("expectation mapping has no content")
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
		a.Params = map[string]interface{}{}
		return nil
	case yaml.MappingNode:
		var raw rawAction
		if err := node.Decode(&raw); err != nil {
			return err
		}
		a.Type = ActionType(raw.Key)
		switch v := raw.Value.(type) {
		case string:
			a.Params = map[string]interface{}{"command": v}
		case map[string]interface{}:
			a.Params = v
		default:
			a.Params = map[string]interface{}{}
		}
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
		e.Params = map[string]interface{}{}
		return nil
	case yaml.MappingNode:
		var raw rawExpectation
		if err := node.Decode(&raw); err != nil {
			return err
		}
		e.Type = ExpectationType(raw.Key)
		switch v := raw.Value.(type) {
		case string:
			e.Params = map[string]interface{}{"contains": v}
		case map[string]interface{}:
			e.Params = v
		default:
			e.Params = map[string]interface{}{}
		}
		return nil
	default:
		return fmt.Errorf("invalid expectation format: %v", node.Kind)
	}
}

func parseDuration(value interface{}) (time.Duration, error) {
	switch v := value.(type) {
	case string:
		return time.ParseDuration(v)
	case int:
		return time.Duration(v) * time.Millisecond, nil
	case float64:
		return time.Duration(v) * time.Millisecond, nil
	default:
		return 0, fmt.Errorf("invalid duration: %T", value)
	}
}

func parseInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid integer: %T", value)
	}
}
