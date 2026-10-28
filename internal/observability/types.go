package observability

import (
	"encoding/json"
	"time"
)

type Scope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SessionID        string
	AgentID          string
	AgentVersions    []int
}

type Window struct {
	Start time.Time
	End   time.Time
}

type Target struct {
	SessionID string
	ThreadID  string
	Segment   string
}

type ListQuery struct {
	Offset int
	Sort   string
}

type SessionActivity struct {
	SessionID   string `json:"session_id"`
	StartedAt   int64  `json:"started_at"`
	LastEventAt int64  `json:"last_event_at"`
	ErrorTurns  int64  `json:"error_turns"`
}

type ActivityList struct {
	Items      []SessionActivity
	NextOffset *int
}

type SessionTrajectory struct {
	SessionID     string  `json:"session_id"`
	Title         *string `json:"title"`
	AgentID       string  `json:"agent_id"`
	AgentVersion  int     `json:"agent_version"`
	Status        string  `json:"status"`
	SubagentCount int     `json:"subagent_count"`
	StartedAt     int64   `json:"started_at"`
	LastEventAt   int64   `json:"last_event_at"`
	DurationMs    int64   `json:"duration_ms"`
	ErrorTurns    int64   `json:"error_turns"`
}

type List struct {
	Items      []SessionTrajectory `json:"items"`
	NextOffset *int                `json:"next_offset"`
}

type Segment struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
}

type Page struct {
	Header   json.RawMessage   `json:"header"`
	Events   []json.RawMessage `json:"events"`
	NextSeq  int64             `json:"next_seq"`
	HasMore  bool              `json:"has_more"`
	Segment  string            `json:"segment"`
	Segments []Segment         `json:"segments"`
}

type nativeHeader struct {
	Version   int    `json:"version"`
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`
}

type nativeEvent struct {
	Seq  *int64          `json:"seq"`
	Time int64           `json:"time"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}
