package observability

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type PanelResult struct {
	QueryRef   string    `json:"query_ref"`
	RenderType string    `json:"render_type"`
	DataAsOf   time.Time `json:"data_as_of"`
	Data       any       `json:"data"`
}

type ResourceStore interface {
	GetAgent(ctx context.Context, workspaceUUID, agentID string) error
	GetSession(ctx context.Context, workspaceUUID, sessionID string) error
}

type DBStore struct {
	DB *db.DB
}

func (s DBStore) GetAgent(ctx context.Context, workspaceUUID, agentID string) error {
	_, err := s.DB.GetAgent(ctx, workspaceUUID, agentID)
	return err
}

func (s DBStore) GetSession(ctx context.Context, workspaceUUID, sessionID string) error {
	_, found, err := s.DB.GetSession(ctx, workspaceUUID, sessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	return nil
}

type Handler struct {
	backend QueryBackend
	store   ResourceStore
	logger  *slog.Logger
	now     func() time.Time
}

func NewHandler(backend QueryBackend, store ResourceStore, logger *slog.Logger) *Handler {
	return &Handler{
		backend: backend,
		store:   store,
		logger:  logging.LoggerOrDefault(logger),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (h *Handler) Dashboard(_ context.Context) (DashboardProjection, error) {
	return projectDashboard(), nil
}

func (h *Handler) QueryPanel(ctx context.Context, scope TenantScope, queryRef string, variables map[string]any) (PanelResult, error) {
	queryRef = strings.TrimSpace(queryRef)
	if queryRef == "" {
		return PanelResult{}, errQueryRefRequired()
	}
	spec, ok := querySpec(queryRef)
	if !ok {
		return PanelResult{}, errQueryRefNotFound(queryRef)
	}
	// trace.list / trace.detail 等无渲染类型的固定查询不允许走 Panel 接口。
	renderType, ok := QueryRenderType(queryRef)
	if !ok {
		return PanelResult{}, errQueryRefNotFound(queryRef)
	}
	bound, err := bindVariables(spec.Variables, variables, scope)
	if err != nil {
		return PanelResult{}, err
	}
	if err := h.ensureOwnership(ctx, bound.Scope); err != nil {
		return PanelResult{}, err
	}
	rows, err := h.backend.PanelRows(ctx, queryRef, bound)
	if err != nil {
		return PanelResult{}, err
	}
	data, err := mapPanelRows(renderType, rows)
	if err != nil {
		return PanelResult{}, err
	}
	return PanelResult{
		QueryRef:   queryRef,
		RenderType: renderType,
		DataAsOf:   h.now(),
		Data:       data,
	}, nil
}

func (h *Handler) ListTraces(ctx context.Context, scope TenantScope, variables map[string]any, offset int) (TraceListResult, error) {
	if offset < 0 {
		return TraceListResult{}, errInvalidOffset()
	}
	spec, ok := querySpec(traceListRef)
	if !ok {
		return TraceListResult{}, errQueryRefNotFound(traceListRef)
	}
	bound, err := bindVariables(spec.Variables, variables, scope)
	if err != nil {
		return TraceListResult{}, err
	}
	bound.Offset = offset
	status := ""
	if value, ok := bound.Values["status"]; ok {
		status, err = normalizeTraceStatus(value.Str)
		if err != nil {
			return TraceListResult{}, err
		}
	}
	if err := h.ensureOwnership(ctx, bound.Scope); err != nil {
		return TraceListResult{}, err
	}
	traceID := ""
	if value, ok := bound.Values["trace_id"]; ok {
		traceID = value.Str
	}
	rows, err := h.backend.TraceListRows(ctx, TraceListQuery{
		Bound:   bound,
		TraceID: traceID,
		Status:  status,
		Offset:  offset,
	})
	if err != nil {
		return TraceListResult{}, err
	}
	items, hasMore, err := mapTraceList(rows)
	if err != nil {
		return TraceListResult{}, err
	}
	return TraceListResult{DataAsOf: h.now(), HasMore: hasMore, Items: items}, nil
}

func (h *Handler) GetTrace(ctx context.Context, scope TenantScope, traceID string, variables map[string]any) (TraceDetailResult, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return TraceDetailResult{}, errTraceIDRequired()
	}
	spec, ok := querySpec(traceDetailRef)
	if !ok {
		return TraceDetailResult{}, errQueryRefNotFound(traceDetailRef)
	}
	if variables == nil {
		variables = map[string]any{}
	}
	variables["trace_id"] = traceID
	bound, err := bindVariables(spec.Variables, variables, scope)
	if err != nil {
		return TraceDetailResult{}, err
	}
	if err := h.ensureOwnership(ctx, bound.Scope); err != nil {
		return TraceDetailResult{}, err
	}
	spans, err := h.backend.TraceSpans(ctx, TraceDetailQuery{Bound: bound, TraceID: traceID})
	if err != nil {
		return TraceDetailResult{}, err
	}
	return TraceDetailResult{TraceID: traceID, DataAsOf: h.now(), Spans: mapTraceSpans(spans)}, nil
}

func (h *Handler) ensureOwnership(ctx context.Context, scope TenantScope) error {
	if h.store == nil {
		return errInternal("observability resource store is not configured", nil)
	}
	if strings.TrimSpace(scope.OrganizationUUID) == "" || strings.TrimSpace(scope.WorkspaceUUID) == "" {
		return errInternal("observability scope is missing organization or workspace", nil)
	}
	if agentID := strings.TrimSpace(scope.AgentID); agentID != "" {
		if err := h.store.GetAgent(ctx, scope.WorkspaceUUID, agentID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return errAgentNotFound()
			}
			return errInternal("lookup agent", err)
		}
	}
	if sessionID := strings.TrimSpace(scope.SessionID); sessionID != "" {
		if err := h.store.GetSession(ctx, scope.WorkspaceUUID, sessionID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return errSessionNotFound()
			}
			return errInternal("lookup session", err)
		}
	}
	return nil
}

func ParseOffset(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errInvalidOffset()
	}
	return offset, nil
}

func InvalidRequestBody() error {
	return errInvalidRequestBody()
}

func OrganizationNotFound() error {
	return errNotFound("organization not found")
}

func WorkspaceRequired() error {
	return errInternal("workspace is required", nil)
}
