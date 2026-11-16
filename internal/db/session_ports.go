package db

import (
	"context"
	"time"
)

type SessionPortTarget struct {
	SessionUUID           string
	SessionExternalID     string
	OrganizationUUID      string
	WorkspaceUUID         string
	Removed               bool
	CodeSessionUUID       string
	CodeSessionExternalID string
	ProviderSandboxID     string
	SandboxState          string
}

func (d *DB) FindSessionPortTarget(ctx context.Context, sessionUUID string) (SessionPortTarget, error) {
	row, found, err := NewSessionPortMapper(d.mapperDB).FindTarget(ctx, sessionUUID)
	if err != nil {
		return SessionPortTarget{}, err
	}
	if !found {
		return SessionPortTarget{}, ErrNotFound
	}
	return SessionPortTarget(row), nil
}

func (d *DB) TouchCodeSessionPortAccess(ctx context.Context, organizationUUID, workspaceUUID, codeSessionUUID string, now time.Time, interval time.Duration) error {
	return NewSessionPortMapper(d.mapperDB).TouchPortLastAccess(ctx, sessionPortAccessParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		CodeSessionUUID:  codeSessionUUID,
		Now:              now,
		Threshold:        now.Add(-interval),
	})
}
