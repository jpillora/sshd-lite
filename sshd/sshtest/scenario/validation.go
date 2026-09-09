package scenario

import (
	"fmt"
	"math"
	"time"
)

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
