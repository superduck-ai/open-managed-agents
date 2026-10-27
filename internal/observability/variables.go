package observability

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

type variableType string

const (
	variableTime       variableType = "time"
	variableString     variableType = "string"
	variableStringList variableType = "string_list"
	variableIntList    variableType = "int_list"

	maxQuerySpan = 30 * 24 * time.Hour
)

type variableSpec struct {
	Name     string       `json:"name"`
	Type     variableType `json:"type"`
	Required bool         `json:"required"`
}

var reservedVariableNames = map[string]struct{}{
	"oma_organization_uuid": {},
	"oma_workspace_uuid":    {},
	"start_us":              {},
	"end_us":                {},
	"prev_start_us":         {},
	"bucket_interval":       {},
	"offset":                {},
	"scope":                 {},
}

func bindVariables(specs []variableSpec, raw map[string]any, scope TenantScope) (BoundVariables, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	declared := make(map[string]variableSpec, len(specs))
	for _, spec := range specs {
		declared[spec.Name] = spec
	}
	for name := range raw {
		if _, reserved := reservedVariableNames[name]; reserved {
			return BoundVariables{}, errInvalidArgument(fmt.Sprintf("variable %q is not declared", name))
		}
		if _, ok := declared[name]; !ok {
			return BoundVariables{}, errInvalidArgument(fmt.Sprintf("variable %q is not declared", name))
		}
	}
	values := make(map[string]TypedValue, len(specs))
	for _, spec := range specs {
		value, present := raw[spec.Name]
		if !present || value == nil {
			if spec.Required {
				return BoundVariables{}, errInvalidArgument(fmt.Sprintf("variable %q is required", spec.Name))
			}
			continue
		}
		typed, err := parseTypedValue(spec, value)
		if err != nil {
			return BoundVariables{}, err
		}
		values[spec.Name] = typed
	}
	window, err := timeWindowFromValues(values)
	if err != nil {
		return BoundVariables{}, err
	}
	if strings.TrimSpace(scope.OrganizationUUID) == "" || strings.TrimSpace(scope.WorkspaceUUID) == "" {
		return BoundVariables{}, errInternal("observability scope is missing organization or workspace", nil)
	}
	if agentID, ok := values["agent_id"]; ok {
		scope.AgentID = agentID.Str
	}
	if sessionID, ok := values["session_id"]; ok {
		scope.SessionID = sessionID.Str
	}
	if versions, ok := values["agent_version"]; ok {
		scope.AgentVersions = uniqueSortedInts(versions.Ints)
	}
	return BoundVariables{
		Window:         window,
		BucketInterval: bucketInterval(window.End.Sub(window.Start)),
		Scope:          scope,
		Values:         values,
	}, nil
}

func parseTypedValue(spec variableSpec, value any) (TypedValue, error) {
	switch spec.Type {
	case variableTime:
		raw, ok := value.(string)
		if !ok {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be an RFC3339 timestamp", spec.Name))
		}
		parsed, err := parseRFC3339(raw)
		if err != nil {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be an RFC3339 timestamp", spec.Name))
		}
		return TypedValue{Type: variableTime, Time: parsed}, nil
	case variableString:
		raw, ok := value.(string)
		if !ok {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be a string", spec.Name))
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be a non-empty string", spec.Name))
		}
		return TypedValue{Type: variableString, Str: trimmed}, nil
	case variableStringList:
		list, err := stringListValue(value)
		if err != nil || len(list) == 0 {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be a non-empty string list", spec.Name))
		}
		return TypedValue{Type: variableStringList, List: list}, nil
	case variableIntList:
		list, err := intListValue(value)
		if err != nil || len(list) == 0 {
			return TypedValue{}, errInvalidArgument(fmt.Sprintf("variable %q must be a non-empty integer list", spec.Name))
		}
		return TypedValue{Type: variableIntList, Ints: list}, nil
	default:
		return TypedValue{}, errInternal(fmt.Sprintf("unsupported variable type %q", spec.Type), nil)
	}
}

func stringListValue(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return nonEmptyStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			raw, ok := item.(string)
			if !ok {
				return nil, errInvalidArgument("string list items must be strings")
			}
			out = append(out, raw)
		}
		return nonEmptyStrings(out)
	default:
		return nil, errInvalidArgument("string list items must be strings")
	}
}

func intListValue(value any) ([]int64, error) {
	switch typed := value.(type) {
	case []int64:
		return nonEmptyInts(typed)
	case []int:
		out := make([]int64, 0, len(typed))
		for _, item := range typed {
			out = append(out, int64(item))
		}
		return nonEmptyInts(out)
	case []any:
		out := make([]int64, 0, len(typed))
		for _, item := range typed {
			n, err := parseIntItem(item)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return nonEmptyInts(out)
	default:
		return nil, errInvalidArgument("int list items must be integers")
	}
}

func parseIntItem(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) {
			return 0, errInvalidArgument("int list items must be integers")
		}
		if typed > float64(math.MaxInt64) || typed < float64(math.MinInt64) {
			return 0, errInvalidArgument("int list items must be integers")
		}
		return int64(typed), nil
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return 0, errInvalidArgument("int list items must be integers")
		}
		return n, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, errInvalidArgument("int list items must be integers")
		}
		return n, nil
	default:
		return 0, errInvalidArgument("int list items must be integers")
	}
}

func nonEmptyInts(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, errInvalidArgument("int list must not be empty")
	}
	return append([]int64(nil), values...), nil
}

func uniqueSortedInts(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	out := append([]int64(nil), values...)
	slices.Sort(out)
	n := 0
	for _, value := range out {
		if n == 0 || out[n-1] != value {
			out[n] = value
			n++
		}
	}
	return out[:n]
}

func nonEmptyStrings(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, errInvalidArgument("string list items must be non-empty")
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, errInvalidArgument("string list must not be empty")
	}
	return out, nil
}

func timeWindowFromValues(values map[string]TypedValue) (TimeWindow, error) {
	start, okStart := values["start_time"]
	end, okEnd := values["end_time"]
	if !okStart && !okEnd {
		return TimeWindow{}, nil
	}
	if !okStart || !okEnd {
		return TimeWindow{}, errInvalidArgument("start_time and end_time are required")
	}
	if !end.Time.After(start.Time) {
		return TimeWindow{}, errInvalidArgument("end_time must be after start_time")
	}
	if end.Time.Sub(start.Time) > maxQuerySpan {
		return TimeWindow{}, errInvalidArgument("time range must be at most 30 days")
	}
	span := end.Time.Sub(start.Time)
	return TimeWindow{
		Start:     start.Time,
		End:       end.Time,
		PrevStart: start.Time.Add(-span),
	}, nil
}

func bucketInterval(span time.Duration) time.Duration {
	switch {
	case span <= 15*time.Minute:
		return 30 * time.Second
	case span <= time.Hour:
		return time.Minute
	case span <= 6*time.Hour:
		return 5 * time.Minute
	case span <= 24*time.Hour:
		return 30 * time.Minute
	case span <= 7*24*time.Hour:
		return 3 * time.Hour
	default:
		return 12 * time.Hour
	}
}

func parseRFC3339(raw string) (time.Time, error) {
	// time.RFC3339 解析时本身接受小数秒，无需再回退 RFC3339Nano。
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
