package platform

import "time"

type WorkbenchPromptRecord struct {
	OrgUUID               string
	PromptUUID            string
	WorkspaceID           string
	Name                  string
	IsSharedWithWorkspace bool
	LatestRevisionUUID    *string
	DeletedAt             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type WorkbenchRevisionRecord struct {
	OrgUUID      string
	PromptUUID   string
	RevisionUUID string
	Payload      map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type WorkbenchKVRecord struct {
	OrgUUID    string
	PromptUUID string
	Key        string
	Value      string
	Version    any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type WorkbenchEvaluationRecord struct {
	OrgUUID        string
	RevisionUUID   string
	EvaluationUUID string
	Payload        map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
