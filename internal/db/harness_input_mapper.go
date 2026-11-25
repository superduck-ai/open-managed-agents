package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper HarnessInputMapper -sql ./harness_input_mapper.xml -out ./harness_input_mapper.sqlmap.gen.go -dialect postgres

type harnessInputRow struct {
	InputUUID        string     `db:"input_uuid"`
	SessionUUID      string     `db:"session_uuid"`
	OrganizationUUID string     `db:"organization_uuid"`
	WorkspaceUUID    string     `db:"workspace_uuid"`
	InputMessage     []byte     `db:"input_message"`
	State            string     `db:"state"`
	PublishedAt      *time.Time `db:"published_at"`
}

type HarnessInputMapper interface {
	Insert(ctx context.Context, ref HarnessTurnRef, inputMessage string, createdAt time.Time) error
	Find(ctx context.Context, ref HarnessTurnRef) (harnessInputRow, bool, error)
	FindFirstPendingInput(ctx context.Context, ref HarnessTurnRef) (string, bool, error)
	ListUnpublished(ctx context.Context, limit int) ([]harnessInputRow, error)
	ListUnpublishedForSession(ctx context.Context, ref HarnessTurnRef) ([]harnessInputRow, error)
	MarkPublished(ctx context.Context, ref HarnessTurnRef) error
	SetQueuedState(ctx context.Context, ref HarnessTurnRef, state string) (int64, error)
	DeleteBySession(ctx context.Context, ref HarnessTurnRef) error
}
