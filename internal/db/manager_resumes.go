package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

type ManagerResumeTarget struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SandboxUUID      string
	SessionID        string
	CodeSessionID    string
	WorkDir          string
	Transport        string
	Version          string
	Phase            string
}

func (d *DB) FindManagerResumeTarget(ctx context.Context, workspaceUUID, providerID string) (ManagerResumeTarget, bool, error) {
	row, found, err := NewManagerResumeMapper(d.mapperDB).Find(ctx, workspaceUUID, providerID)
	return ManagerResumeTarget{
		OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, SandboxUUID: row.SandboxUUID,
		SessionID: row.SessionID, CodeSessionID: row.CodeSessionID, WorkDir: row.WorkDir,
		Transport: row.Transport, Version: row.Version, Phase: row.Phase,
	}, found, err
}

func (d *DB) WithManagerResumeLock(ctx context.Context, sandboxUUID string, resume func() error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		locked, err := NewManagerResumeMapper(executor).TryLock(ctx, sandboxUUID)
		if err != nil {
			return err
		}
		if !locked {
			return ErrPreconditionFailed
		}
		return resume()
	})
}

func (d *DB) SetManagerResumePhase(ctx context.Context, target ManagerResumeTarget, phase string) error {
	rows, err := NewManagerResumeMapper(d.mapperDB).SetPhase(ctx, target, phase)
	if err == nil && rows != 1 {
		return ErrInvalidState
	}
	return err
}

func (d *DB) IsManagerResumeIdle(ctx context.Context, target ManagerResumeTarget) (bool, error) {
	return NewManagerResumeMapper(d.mapperDB).IsIdle(ctx, target)
}

func (d *DB) RecordManagerVersion(ctx context.Context, target ManagerResumeTarget, version string) error {
	rows, err := NewManagerResumeMapper(d.mapperDB).RecordVersion(ctx, target, version)
	if err == nil && rows != 1 {
		return ErrInvalidState
	}
	return err
}
