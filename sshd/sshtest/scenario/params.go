package scenario

import (
	"fmt"
)

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
