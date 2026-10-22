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
}

type Window struct {
	Start time.Time
	End   time.Time
}

type Trajectory struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	AgentID     string `json:"agent_id"`
	StartedAt   int64  `json:"started_at"`
	LastEventAt int64  `json:"last_event_at"`
}

type List struct {
	Items      []Trajectory `json:"items"`
	NextOffset *int         `json:"next_offset"`
}

type Page struct {
	Header  json.RawMessage   `json:"header"`
	Events  []json.RawMessage `json:"events"`
	NextSeq int64             `json:"next_seq"`
	HasMore bool              `json:"has_more"`
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
