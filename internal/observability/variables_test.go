package observability

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestBindVariablesRejectsUndeclaredAndReservedNames(t *testing.T) {
	specs := []variableSpec{
		{Name: "start_time", Type: variableTime, Required: true},
		{Name: "end_time", Type: variableTime, Required: true},
	}
	scope := TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	raw := map[string]any{
		"start_time":            "2026-08-12T00:00:00Z",
		"end_time":              "2026-08-13T00:00:00Z",
		"oma_organization_uuid": "spoofed",
	}
	_, err := bindVariables(specs, raw, scope)
	assertInvalidArgument(t, err, "oma_organization_uuid")

	raw = map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
		"extra":      "nope",
	}
	_, err = bindVariables(specs, raw, scope)
	assertInvalidArgument(t, err, "extra")
}

func TestBindVariablesRejectsMissingRequiredAndWrongTypes(t *testing.T) {
	specs := []variableSpec{
		{Name: "start_time", Type: variableTime, Required: true},
		{Name: "end_time", Type: variableTime, Required: true},
		{Name: "model", Type: variableStringList, Required: false},
	}
	scope := TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	_, err := bindVariables(specs, map[string]any{"start_time": "2026-08-12T00:00:00Z"}, scope)
	assertInvalidArgument(t, err, "end_time")

	_, err = bindVariables(specs, map[string]any{
		"start_time": 123,
		"end_time":   "2026-08-13T00:00:00Z",
	}, scope)
	assertInvalidArgument(t, err, "RFC3339")

	_, err = bindVariables(specs, map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
		"model":      []any{},
	}, scope)
	assertInvalidArgument(t, err, "model")
}

func TestBindVariablesRejectsInvalidTimeWindows(t *testing.T) {
	specs := []variableSpec{
		{Name: "start_time", Type: variableTime, Required: true},
		{Name: "end_time", Type: variableTime, Required: true},
	}
	scope := TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	_, err := bindVariables(specs, map[string]any{
		"start_time": "2026-08-13T00:00:00Z",
		"end_time":   "2026-08-12T00:00:00Z",
	}, scope)
	assertInvalidArgument(t, err, "after")

	_, err = bindVariables(specs, map[string]any{
		"start_time": "2026-07-01T00:00:00Z",
		"end_time":   "2026-08-02T00:00:00Z",
	}, scope)
	assertInvalidArgument(t, err, "30 days")
}

func TestBindVariablesDerivesPrevWindowBucketAndScope(t *testing.T) {
	specs := []variableSpec{
		{Name: "start_time", Type: variableTime, Required: true},
		{Name: "end_time", Type: variableTime, Required: true},
		{Name: "agent_id", Type: variableString, Required: false},
		{Name: "session_id", Type: variableString, Required: false},
		{Name: "model", Type: variableStringList, Required: false},
	}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	bound, err := bindVariables(specs, map[string]any{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
		"agent_id":   "agent_01",
		"model":      []any{"claude-sonnet-4"},
	}, TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"})
	if err != nil {
		t.Fatalf("bindVariables() error = %v", err)
	}
	if !bound.Window.Start.Equal(start) || !bound.Window.End.Equal(end) {
		t.Fatalf("window = %+v", bound.Window)
	}
	if !bound.Window.PrevStart.Equal(start.Add(-24 * time.Hour)) {
		t.Fatalf("PrevStart = %s", bound.Window.PrevStart)
	}
	if bound.BucketInterval != 30*time.Minute {
		t.Fatalf("BucketInterval = %s, want 30m", bound.BucketInterval)
	}
	if bound.Scope.OrganizationUUID != "org" || bound.Scope.WorkspaceUUID != "ws" || bound.Scope.AgentID != "agent_01" {
		t.Fatalf("Scope = %+v", bound.Scope)
	}
	if got := bound.Values["model"].List; len(got) != 1 || got[0] != "claude-sonnet-4" {
		t.Fatalf("model = %#v", bound.Values["model"])
	}
}

func TestBindVariablesCopiesAgentVersionsToScope(t *testing.T) {
	specs := []variableSpec{
		{Name: "start_time", Type: variableTime, Required: true},
		{Name: "end_time", Type: variableTime, Required: true},
		{Name: "agent_version", Type: variableIntList, Required: false},
	}
	scope := TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	bound, err := bindVariables(specs, map[string]any{
		"start_time":    "2026-08-12T00:00:00Z",
		"end_time":      "2026-08-13T00:00:00Z",
		"agent_version": []any{4.0, "3", 4},
	}, scope)
	if err != nil {
		t.Fatalf("bindVariables() error = %v", err)
	}
	if got := bound.Scope.AgentVersions; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("AgentVersions = %#v, want [3 4]", got)
	}
	if bound.Values["agent_version"].Type != variableIntList {
		t.Fatalf("type = %q", bound.Values["agent_version"].Type)
	}

	_, err = bindVariables(specs, map[string]any{
		"start_time":    "2026-08-12T00:00:00Z",
		"end_time":      "2026-08-13T00:00:00Z",
		"agent_version": []any{},
	}, scope)
	assertInvalidArgument(t, err, "agent_version")

	_, err = bindVariables(specs, map[string]any{
		"start_time":    "2026-08-12T00:00:00Z",
		"end_time":      "2026-08-13T00:00:00Z",
		"agent_version": []any{3.5},
	}, scope)
	assertInvalidArgument(t, err, "integer")
}

func TestBindVariablesAllowsQueriesWithoutTimeWindow(t *testing.T) {
	specs := []variableSpec{
		{Name: "trace_id", Type: variableString, Required: true},
		{Name: "agent_id", Type: variableString, Required: false},
	}
	bound, err := bindVariables(specs, map[string]any{"trace_id": "abc"}, TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"})
	if err != nil {
		t.Fatalf("bindVariables() error = %v", err)
	}
	if !bound.Window.Start.IsZero() || !bound.Window.End.IsZero() {
		t.Fatalf("window = %+v, want zero", bound.Window)
	}
	if bound.Values["trace_id"].Str != "abc" {
		t.Fatalf("trace_id = %#v", bound.Values["trace_id"])
	}
}

func TestBucketIntervalMapping(t *testing.T) {
	tests := []struct {
		span time.Duration
		want time.Duration
	}{
		{10 * time.Minute, 30 * time.Second},
		{time.Hour, time.Minute},
		{6 * time.Hour, 5 * time.Minute},
		{24 * time.Hour, 30 * time.Minute},
		{7 * 24 * time.Hour, 3 * time.Hour},
		{30 * 24 * time.Hour, 12 * time.Hour},
	}
	for _, test := range tests {
		if got := bucketInterval(test.span); got != test.want {
			t.Fatalf("bucketInterval(%s) = %s, want %s", test.span, got, test.want)
		}
	}
}

func assertInvalidArgument(t *testing.T, err error, contains string) {
	t.Helper()
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
	if contains != "" && !strings.Contains(appErr.PublicMessage, contains) {
		t.Fatalf("error = %q, want substring %q", appErr.PublicMessage, contains)
	}
}
