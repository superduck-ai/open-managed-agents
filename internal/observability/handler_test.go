package observability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeBackend struct {
	panelFn func(ctx context.Context, queryRef string, bound BoundVariables) ([]Row, error)
	listFn  func(ctx context.Context, q TraceListQuery) ([]Row, error)
	spanFn  func(ctx context.Context, q TraceDetailQuery) (TraceSpansResult, error)
	last    BoundVariables
}

func (f *fakeBackend) PanelRows(ctx context.Context, queryRef string, bound BoundVariables) ([]Row, error) {
	f.last = bound
	if f.panelFn != nil {
		return f.panelFn(ctx, queryRef, bound)
	}
	return nil, nil
}

func (f *fakeBackend) TraceListRows(ctx context.Context, q TraceListQuery) ([]Row, error) {
	f.last = q.Bound
	if f.listFn != nil {
		return f.listFn(ctx, q)
	}
	return nil, nil
}

func (f *fakeBackend) TraceSpans(ctx context.Context, q TraceDetailQuery) (TraceSpansResult, error) {
	f.last = q.Bound
	if f.spanFn != nil {
		return f.spanFn(ctx, q)
	}
	return TraceSpansResult{}, nil
}

type fakeStore struct {
	agents   map[string]error
	sessions map[string]error
}

func (s fakeStore) GetAgent(_ context.Context, _, agentID string) error {
	if err, ok := s.agents[agentID]; ok {
		return err
	}
	return db.ErrNotFound
}

func (s fakeStore) GetSession(_ context.Context, _, sessionID string) error {
	if err, ok := s.sessions[sessionID]; ok {
		return err
	}
	return db.ErrNotFound
}

func TestHandlerQueryPanelInjectsScopeAndMapsRows(t *testing.T) {
	backend := &fakeBackend{panelFn: func(_ context.Context, queryRef string, bound BoundVariables) ([]Row, error) {
		if queryRef != "overview.interactions" {
			t.Fatalf("queryRef = %s", queryRef)
		}
		if bound.Scope.OrganizationUUID != "org" || bound.Scope.WorkspaceUUID != "ws" {
			t.Fatalf("scope = %+v", bound.Scope)
		}
		return []Row{{"current": 4.0, "previous": 2.0, "change_percent": 100.0}}, nil
	}}
	handler := NewHandler(backend, fakeStore{}, nil)
	handler.now = func() time.Time { return time.Date(2026, 8, 13, 0, 0, 3, 0, time.UTC) }
	result, err := handler.QueryPanel(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "overview.interactions", map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("QueryPanel() error = %v", err)
	}
	if result.RenderType != renderStat || result.QueryRef != "overview.interactions" {
		t.Fatalf("result = %+v", result)
	}
	stat, ok := result.Data.(StatData)
	if !ok || stat.Current == nil || *stat.Current != 4 {
		t.Fatalf("data = %#v", result.Data)
	}
	if backend.last.Scope.OrganizationUUID != "org" || backend.last.Scope.WorkspaceUUID != "ws" {
		t.Fatalf("backend scope = %+v", backend.last.Scope)
	}
}

func TestHandlerQueryPanelUnknownRefAndOwnership(t *testing.T) {
	handler := NewHandler(&fakeBackend{}, fakeStore{agents: map[string]error{}}, nil)
	_, err := handler.QueryPanel(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "does.not.exist", map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
	})
	assertNotFound(t, err, "does.not.exist")

	_, err = handler.QueryPanel(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "overview.interactions", map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
		"agent_id":   "agent_missing",
	})
	assertNotFound(t, err, "agent not found")
}

func TestHandlerRejectsClientScopeOverride(t *testing.T) {
	handler := NewHandler(&fakeBackend{}, fakeStore{}, nil)
	_, err := handler.QueryPanel(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "overview.interactions", map[string]any{
		"start_time":            "2026-08-12T00:00:00Z",
		"end_time":              "2026-08-13T00:00:00Z",
		"oma_organization_uuid": "spoof",
	})
	assertInvalidArgument(t, err, "oma_organization_uuid")
}

func TestHandlerListAndGetTrace(t *testing.T) {
	start := time.Date(2026, 8, 12, 10, 20, 30, 0, time.UTC)
	backend := &fakeBackend{
		listFn: func(_ context.Context, q TraceListQuery) ([]Row, error) {
			if q.Offset != 0 || q.Status != "ok" {
				t.Fatalf("query = %+v", q)
			}
			return []Row{{
				"trace_id": "abc", "session_id": "sess", "start_time": start,
				"duration_ms": 12.0, "tokens": 3.0, "llm_calls": 1.0, "tool_calls": 0.0, "has_error": 0.0,
			}}, nil
		},
		spanFn: func(_ context.Context, q TraceDetailQuery) (TraceSpansResult, error) {
			if q.TraceID != "abc" {
				t.Fatalf("trace id = %s", q.TraceID)
			}
			return TraceSpansResult{
				Spans:     []Span{{SpanID: "s1", Kind: "interaction", Name: "claude_code.interaction", Status: "ok"}},
				Truncated: true,
			}, nil
		},
	}
	handler := NewHandler(backend, fakeStore{sessions: map[string]error{"sess_01": nil}}, nil)
	handler.now = func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) }
	list, err := handler.ListTraces(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
		"status":     "ok",
	}, 0)
	if err != nil || len(list.Items) != 1 || list.Items[0].TraceID != "abc" {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	detail, err := handler.GetTrace(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "abc", nil)
	if err != nil || len(detail.Spans) != 1 || detail.TraceID != "abc" || !detail.Truncated {
		t.Fatalf("detail = %+v err=%v", detail, err)
	}
	wantStart, wantEnd := handler.now().Add(-maxQuerySpan), handler.now().Add(time.Hour)
	if !backend.last.Window.Start.Equal(wantStart) || !backend.last.Window.End.Equal(wantEnd) {
		t.Fatalf("detail window = %+v, want %s to %s", backend.last.Window, wantStart, wantEnd)
	}
	_, err = handler.GetTrace(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "abc", map[string]any{"start_time": "2026-08-12T00:00:00Z"})
	assertInvalidArgument(t, err, "start_time")
	_, err = handler.GetTrace(context.Background(), TenantScope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, "abc", map[string]any{
		"start_time": "2026-08-12T00:00:00Z",
		"end_time":   "2026-08-13T00:00:00Z",
	})
	if err != nil || !backend.last.Window.Start.Equal(time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("bounded detail window = %+v err=%v", backend.last.Window, err)
	}
}

func assertNotFound(t *testing.T, err error, contains string) {
	t.Helper()
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.NotFound {
		t.Fatalf("error = %v, want NotFound", err)
	}
	if contains != "" && !strings.Contains(appErr.PublicMessage, contains) {
		t.Fatalf("error = %q, want substring %q", appErr.PublicMessage, contains)
	}
}
