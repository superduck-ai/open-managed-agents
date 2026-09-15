package openobserve

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

var (
	scopePlaceholderPattern = regexp.MustCompile(`\{scope(?::([A-Za-z_][A-Za-z0-9_]*))?\}`)
	placeholderPattern      = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*(?::[A-Za-z_][A-Za-z0-9_]*)?\}`)
	namedPlaceholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)(?::([A-Za-z_][A-Za-z0-9_]*))?\}`)
)

type streamType string

const (
	streamTraces  streamType = "traces"
	streamMetrics streamType = "metrics"
	streamLogs    streamType = "logs"
)

type renderExtras struct {
	traceIDFilter string
	statusHaving  string
}

func renderSQL(template string, bound observability.BoundVariables, signal streamType, extras renderExtras) (string, error) {
	if !scopePlaceholderPattern.MatchString(template) {
		return "", observability.MissingScopePlaceholder()
	}
	var err error
	out := scopePlaceholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		alias := ""
		parts := namedPlaceholderPattern.FindStringSubmatch(match)
		if len(parts) == 3 {
			alias = parts[2]
		}
		rendered, scopeErr := renderScope(bound.Scope, signal, alias)
		if scopeErr != nil {
			err = scopeErr
			return match
		}
		return rendered
	})
	if err != nil {
		return "", err
	}
	replacements, err := literalReplacements(bound, extras)
	if err != nil {
		return "", err
	}
	out = namedPlaceholderPattern.ReplaceAllStringFunc(out, func(match string) string {
		parts := namedPlaceholderPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		name, alias := parts[1], parts[2]
		if name == "model_filter" || name == "tool_filter" {
			literal, filterErr := renderFilter(name, alias, bound)
			if filterErr != nil {
				err = filterErr
				return match
			}
			return literal
		}
		if alias != "" {
			return match
		}
		if replacement, ok := replacements[name]; ok {
			return replacement
		}
		return match
	})
	if err != nil {
		return "", err
	}
	if leftover := placeholderPattern.FindString(out); leftover != "" {
		return "", observability.UnresolvedPlaceholder(leftover)
	}
	return out, nil
}

func literalReplacements(bound observability.BoundVariables, extras renderExtras) (map[string]string, error) {
	replacements := map[string]string{
		"start_us":        fmt.Sprintf("%d", bound.Window.Start.UnixMicro()),
		"end_us":          fmt.Sprintf("%d", bound.Window.End.UnixMicro()),
		"prev_start_us":   fmt.Sprintf("%d", bound.Window.PrevStart.UnixMicro()),
		"bucket_interval": formatBucketInterval(bound.BucketInterval),
		"offset":          fmt.Sprintf("%d", bound.Offset),
		"trace_id_filter": extras.traceIDFilter,
		"status_having":   extras.statusHaving,
	}
	for name, value := range bound.Values {
		switch {
		case len(value.Ints) > 0:
			replacements[name] = quoteIntList(value.Ints)
		case len(value.List) > 0:
			literal, err := quoteStringList(value.List)
			if err != nil {
				return nil, err
			}
			replacements[name] = literal
		case !value.Time.IsZero():
			replacements[name] = fmt.Sprintf("%d", value.Time.UnixMicro())
		default:
			quoted, err := quoteString(value.Str)
			if err != nil {
				return nil, err
			}
			replacements[name] = quoted
		}
	}
	return replacements, nil
}

func renderFilter(name, alias string, bound observability.BoundVariables) (string, error) {
	column := "model"
	valueName := "model"
	if name == "tool_filter" {
		column = "tool_name"
		valueName = "tool"
	}
	if alias != "" {
		column = alias + "." + column
	}
	value, ok := bound.Values[valueName]
	if !ok || len(value.List) == 0 {
		return "1=1", nil
	}
	list, err := quoteStringList(value.List)
	if err != nil {
		return "", err
	}
	return column + " IN " + list, nil
}

// renderScope 渲染租户过滤条件。作用域值来自认证上下文而非客户端，引用失败意味着
// 服务端缺陷，必须拒绝执行而不是降级成空字符串过滤。
func renderScope(scope observability.TenantScope, signal streamType, alias string) (string, error) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	orgCol, wsCol, agentCol, sessionCol, versionCol := tenantColumns(signal)
	parts := make([]string, 0, 5)
	appendCondition := func(column, value string) error {
		quoted, err := quoteString(value)
		if err != nil {
			return observability.QueryInternal("render tenant scope value for "+column, err)
		}
		parts = append(parts, prefix+column+" = "+quoted)
		return nil
	}
	if err := appendCondition(orgCol, scope.OrganizationUUID); err != nil {
		return "", err
	}
	if err := appendCondition(wsCol, scope.WorkspaceUUID); err != nil {
		return "", err
	}
	if agentID := strings.TrimSpace(scope.AgentID); agentID != "" {
		if err := appendCondition(agentCol, agentID); err != nil {
			return "", err
		}
	}
	if sessionID := strings.TrimSpace(scope.SessionID); sessionID != "" {
		if err := appendCondition(sessionCol, sessionID); err != nil {
			return "", err
		}
	}
	if len(scope.AgentVersions) > 0 {
		parts = append(parts, prefix+versionCol+" IN "+quoteIntList(scope.AgentVersions))
	}
	return strings.Join(parts, " AND "), nil
}

func tenantColumns(signal streamType) (org, workspace, agent, session, version string) {
	if signal == streamTraces {
		return "service_oma_organization_uuid", "service_oma_workspace_uuid", "service_oma_agent_id", "service_oma_session_id", "service_oma_agent_version"
	}
	return "oma_organization_uuid", "oma_workspace_uuid", "oma_agent_id", "oma_session_id", "oma_agent_version"
}

func quoteString(value string) (string, error) {
	for _, r := range value {
		if r < 32 || r == 127 || unicode.IsControl(r) {
			return "", observability.InvalidLiteral("query literal contains a control character")
		}
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'", nil
}

func quoteStringList(values []string) (string, error) {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		quoted, err := quoteString(value)
		if err != nil {
			return "", err
		}
		parts = append(parts, quoted)
	}
	return "(" + strings.Join(parts, ",") + ")", nil
}

func quoteIntList(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return "(" + strings.Join(parts, ",") + ")"
}

func formatBucketInterval(interval time.Duration) string {
	switch interval {
	case 30 * time.Second:
		return "30 seconds"
	case time.Minute:
		return "1 minute"
	case 5 * time.Minute:
		return "5 minutes"
	case 30 * time.Minute:
		return "30 minutes"
	case 3 * time.Hour:
		return "3 hours"
	default:
		return "12 hours"
	}
}

func traceIDFilter(traceID string) (string, error) {
	if strings.TrimSpace(traceID) == "" {
		return "1=1", nil
	}
	quoted, err := quoteString(traceID)
	if err != nil {
		return "", err
	}
	return "trace_id = " + quoted, nil
}

func statusHaving(status string) string {
	switch status {
	case "error":
		return "HAVING has_error = 1"
	case "ok":
		return "HAVING has_error = 0"
	default:
		return ""
	}
}
