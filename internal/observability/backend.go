package observability

import (
	"context"
	"time"
)

// Row is a backend-neutral result row keyed by contract column names.
type Row map[string]any

// Span is a backend-neutral trace span.
type Span struct {
	SpanID       string
	ParentSpanID string
	Kind         string
	Name         string
	StartTime    time.Time
	EndTime      time.Time
	DurationMs   float64
	Status       string
	Attributes   map[string]string
	Events       []SpanEvent
}

// SpanEvent is a backend-neutral span event (e.g. Claude Code tool.output).
type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]string
}

// BoundVariables is the validated, typed binding set passed to a QueryBackend.
type BoundVariables struct {
	Window         TimeWindow
	BucketInterval time.Duration
	Scope          TenantScope
	Offset         int
	Values         map[string]TypedValue
}

// TimeWindow is the requested range; stat queries also receive PrevStart.
type TimeWindow struct {
	Start     time.Time
	End       time.Time
	PrevStart time.Time
}

// TenantScope is injected from the authenticated principal and optional filters.
type TenantScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	AgentID          string
	SessionID        string
	AgentVersions    []int64
}

// TypedValue is a single client variable after type checking.
type TypedValue struct {
	Type variableType
	Str  string
	List []string
	Ints []int64
	Time time.Time
}

// TraceListQuery is the adapter input for a trace list query.
type TraceListQuery struct {
	Bound   BoundVariables
	TraceID string
	Status  string
	Offset  int
}

// TraceDetailQuery is the adapter input for a trace detail query.
type TraceDetailQuery struct {
	Bound   BoundVariables
	TraceID string
}

type TraceSpansResult struct {
	Spans     []Span
	Truncated bool
}

// QueryBackend executes one query and returns standard rows or spans.
type QueryBackend interface {
	PanelRows(ctx context.Context, queryRef string, bound BoundVariables) ([]Row, error)
	TraceListRows(ctx context.Context, q TraceListQuery) ([]Row, error)
	TraceSpans(ctx context.Context, q TraceDetailQuery) (TraceSpansResult, error)
}
