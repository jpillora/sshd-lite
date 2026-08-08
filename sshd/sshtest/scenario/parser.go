package scenario

import (
	"fmt"
	"math"
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

func parseActionParams(actionType ActionType, value interface{}) (map[string]interface{}, error) {
	if isParameterlessAction(actionType) {
		if value == nil {
			return map[string]interface{}{}, nil
		}
		if params, ok := value.(map[string]interface{}); ok && len(params) == 0 {
			return params, nil
		}
		return nil, fmt.Errorf("action %q does not accept parameters", actionType)
	}

	if text, ok := value.(string); ok {
		if text == "" {
			return nil, fmt.Errorf("action %q parameter must not be empty", actionType)
		}
		switch actionType {
		case ActionExec:
			return map[string]interface{}{"command": text}, nil
		case ActionInput, ActionLine:
			return map[string]interface{}{"text": text}, nil
		case ActionKey:
			return map[string]interface{}{"key": text}, nil
		case ActionSleep:
			d, err := parseDuration(text)
			if err != nil {
				return nil, fmt.Errorf("parse %s duration: %w", actionType, err)
			}
			return map[string]interface{}{"duration": d}, nil
		case ActionWaitForEvent:
			return map[string]interface{}{"event": text}, nil
		default:
			return nil, fmt.Errorf("action %q requires a mapping", actionType)
		}
	}
	if actionType == ActionSleep {
		if _, isMap := value.(map[string]interface{}); !isMap {
			d, err := parseDuration(value)
			if err != nil {
				return nil, fmt.Errorf("parse %s duration: %w", actionType, err)
			}
			return map[string]interface{}{"duration": d}, nil
		}
	}
	params, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("action %q parameters must be a string or mapping, got %T", actionType, value)
	}
	result := cloneParams(params)
	switch actionType {
	case ActionExec:
		if err := validateFields(actionType, result, []string{"command"}, nil); err != nil {
			return nil, err
		}
		if err := requireString(result, "command"); err != nil {
			return nil, actionFieldError(actionType, err)
		}
	case ActionInput, ActionLine:
		if err := validateFields(actionType, result, []string{"text"}, nil); err != nil {
			return nil, err
		}
		if err := requireString(result, "text"); err != nil {
			return nil, actionFieldError(actionType, err)
		}
	case ActionKey:
		if err := validateFields(actionType, result, []string{"key"}, nil); err != nil {
			return nil, err
		}
		if err := requireString(result, "key"); err != nil {
			return nil, actionFieldError(actionType, err)
		}
	case ActionSleep:
		if err := validateFields(actionType, result, []string{"duration"}, nil); err != nil {
			return nil, err
		}
		d, err := parseDuration(result["duration"])
		if err != nil {
			return nil, fmt.Errorf("action %q field %q: %w", actionType, "duration", err)
		}
		result["duration"] = d
	case ActionResize:
		if err := validateFields(actionType, result, []string{"cols", "rows"}, nil); err != nil {
			return nil, err
		}
		for _, field := range []string{"cols", "rows"} {
			n, err := parseInt(result[field])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("action %q field %q must be a positive integer", actionType, field)
			}
			result[field] = n
		}
	case ActionWaitForEvent:
		if err := validateFields(actionType, result, []string{"event"}, []string{"attrs", "timeout"}); err != nil {
			return nil, err
		}
		if err := requireString(result, "event"); err != nil {
			return nil, actionFieldError(actionType, err)
		}
		if err := normalizeAttrsAndTimeout(result); err != nil {
			return nil, actionFieldError(actionType, err)
		}
	case ActionLocalForward, ActionRemoteForward, ActionSFTPUpload, ActionSFTPDownload:
		if err := validateFields(actionType, result, []string{"local", "remote"}, nil); err != nil {
			return nil, err
		}
		for _, field := range []string{"local", "remote"} {
			if err := requireString(result, field); err != nil {
				return nil, actionFieldError(actionType, err)
			}
		}
	}
	return result, nil
}

func parseExpectationParams(expectationType ExpectationType, value interface{}) (map[string]interface{}, error) {
	if expectationType == ExpectConnected || expectationType == ExpectDisconnected {
		if value == nil {
			return map[string]interface{}{}, nil
		}
		if params, ok := value.(map[string]interface{}); ok && len(params) == 0 {
			return params, nil
		}
		return nil, fmt.Errorf("expectation %q does not accept parameters", expectationType)
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return nil, fmt.Errorf("expectation %q parameter must not be empty", expectationType)
		}
		switch expectationType {
		case ExpectOutput, ExpectStdout, ExpectStderr, ExpectScreen:
			return map[string]interface{}{"contains": text}, nil
		case ExpectOutputMatch:
			return map[string]interface{}{"pattern": text}, nil
		case ExpectEvent, ExpectNoEvent:
			return map[string]interface{}{"event": text}, nil
		case ExpectWaitForOutput:
			return map[string]interface{}{"text": text}, nil
		default:
			return nil, fmt.Errorf("expectation %q requires an integer or mapping", expectationType)
		}
	}
	if expectationType == ExpectExitCode {
		if n, err := parseInt(value); err == nil {
			return map[string]interface{}{"code": n}, nil
		}
	}
	params, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expectation %q parameters have invalid type %T", expectationType, value)
	}
	result := cloneParams(params)
	switch expectationType {
	case ExpectOutput, ExpectStdout, ExpectStderr, ExpectScreen:
		if err := validateExpectationFields(expectationType, result, []string{"contains"}, nil); err != nil {
			return nil, err
		}
		if err := requireString(result, "contains"); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
	case ExpectOutputMatch:
		if err := validateExpectationFields(expectationType, result, []string{"pattern"}, nil); err != nil {
			return nil, err
		}
		if err := requireString(result, "pattern"); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
	case ExpectExitCode:
		if err := validateExpectationFields(expectationType, result, []string{"code"}, nil); err != nil {
			return nil, err
		}
		n, err := parseInt(result["code"])
		if err != nil {
			return nil, fmt.Errorf("expectation %q field %q: %w", expectationType, "code", err)
		}
		result["code"] = n
	case ExpectEvent, ExpectNoEvent:
		if err := validateExpectationFields(expectationType, result, []string{"event"}, []string{"attrs"}); err != nil {
			return nil, err
		}
		if err := requireString(result, "event"); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
		if err := normalizeAttrsAndTimeout(result); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
	case ExpectWaitForOutput:
		if err := validateExpectationFields(expectationType, result, []string{"text"}, []string{"timeout"}); err != nil {
			return nil, err
		}
		if err := requireString(result, "text"); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
		if err := normalizeAttrsAndTimeout(result); err != nil {
			return nil, expectationFieldError(expectationType, err)
		}
	}
	return result, nil
}

func validateFields(actionType ActionType, params map[string]interface{}, required, optional []string) error {
	return validateParamFields("action", string(actionType), params, required, optional)
}

func validateExpectationFields(expectationType ExpectationType, params map[string]interface{}, required, optional []string) error {
	return validateParamFields("expectation", string(expectationType), params, required, optional)
}

func validateParamFields(kind, name string, params map[string]interface{}, required, optional []string) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range append(append([]string{}, required...), optional...) {
		allowed[field] = struct{}{}
	}
	for field := range params {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%s %q has unknown field %q", kind, name, field)
		}
	}
	for _, field := range required {
		if _, ok := params[field]; !ok {
			return fmt.Errorf("%s %q requires field %q", kind, name, field)
		}
	}
	return nil
}

func requireString(params map[string]interface{}, field string) error {
	value, ok := params[field].(string)
	if !ok {
		return fmt.Errorf("field %q must be a string, got %T", field, params[field])
	}
	if value == "" {
		return fmt.Errorf("field %q must not be empty", field)
	}
	return nil
}

func normalizeAttrsAndTimeout(params map[string]interface{}) error {
	if value, ok := params["attrs"]; ok {
		attrs, err := parseStrings(value)
		if err != nil {
			return fmt.Errorf("field %q: %w", "attrs", err)
		}
		if len(attrs)%2 != 0 {
			return fmt.Errorf("field %q must contain key/value pairs, got %d items", "attrs", len(attrs))
		}
		params["attrs"] = attrs
	}
	if value, ok := params["timeout"]; ok {
		d, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("field %q: %w", "timeout", err)
		}
		params["timeout"] = d
	}
	return nil
}

func actionFieldError(actionType ActionType, err error) error {
	return fmt.Errorf("action %q %w", actionType, err)
}

func expectationFieldError(expectationType ExpectationType, err error) error {
	return fmt.Errorf("expectation %q %w", expectationType, err)
}

func cloneParams(params map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(params))
	for key, value := range params {
		result[key] = value
	}
	return result
}

func parseStrings(value interface{}) ([]string, error) {
	switch values := value.(type) {
	case []string:
		return values, nil
	case []interface{}:
		result := make([]string, len(values))
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("item %d is %T, not string", i, value)
			}
			result[i] = text
		}
		return result, nil
	default:
		return nil, fmt.Errorf("invalid string list: %T", value)
	}
}

func parseDuration(value interface{}) (time.Duration, error) {
	switch v := value.(type) {
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, err
		}
		if d <= 0 {
			return 0, fmt.Errorf("duration must be positive, got %q", v)
		}
		return d, nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("duration must be positive, got %d", v)
		}
		if int64(v) > math.MaxInt64/int64(time.Millisecond) {
			return 0, fmt.Errorf("duration milliseconds overflow time.Duration: %d", v)
		}
		return time.Duration(v) * time.Millisecond, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("duration milliseconds must be finite, got %v", v)
		}
		if v <= 0 {
			return 0, fmt.Errorf("duration must be positive, got %v", v)
		}
		nanoseconds := v * float64(time.Millisecond)
		// float64(math.MaxInt64) rounds to 1<<63, which is already outside
		// time.Duration. Reject that boundary as well as larger values.
		if math.IsInf(nanoseconds, 0) || nanoseconds >= float64(math.MaxInt64) {
			return 0, fmt.Errorf("duration milliseconds overflow time.Duration: %v", v)
		}
		d := time.Duration(nanoseconds)
		if d == 0 {
			return 0, fmt.Errorf("duration milliseconds underflow time.Duration: %v", v)
		}
		return d, nil
	default:
		return 0, fmt.Errorf("invalid duration: %T", value)
	}
}

func parseInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("invalid non-integral number: %v", v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid integer: %T", value)
	}
}
