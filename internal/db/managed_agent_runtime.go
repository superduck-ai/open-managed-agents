package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

// RotateManagedAgentCodeSessionCredentials replaces the one-way OAuth token
// hash before a replacement sandbox starts and revokes the dead worker lease.
func (d *DB) RotateManagedAgentCodeSessionCredentials(
	ctx context.Context,
	session Session,
	codeSessionExternalID string,
	oauthAccessTokenHash string,
) (int64, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	workerEpoch, err := mapper.RotateCredentials(ctx, rotateCodeSessionCredentialsParams{
		OrganizationUUID:      session.OrganizationUUID,
		WorkspaceUUID:         session.WorkspaceUUID,
		SessionExternalID:     session.ExternalID,
		CodeSessionExternalID: codeSessionExternalID,
		OAuthAccessTokenHash:  oauthAccessTokenHash,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return workerEpoch, nil
}

// BindManagedAgentRuntimeMetadata 将已启动的 Managed Agent runtime 信息同时发布到
// public Session 和调用方指定的 Environment Work。
//
// 两个 patch 应是 JSON 对象；PostgreSQL 的 jsonb 顶层合并会保留已有的无关字段，
// 并用 patch 覆盖同名字段。两次更新位于同一个事务中，任一目标不存在或 patch
// 不是合法 JSON 时都会回滚。
func (d *DB) BindManagedAgentRuntimeMetadata(
	ctx context.Context,
	session Session,
	work EnvironmentWork,
	sessionPatch json.RawMessage,
	workPatch json.RawMessage,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		workMapper := NewEnvironmentWorkMapper(executor)

		sessionRows, err := sessionMapper.MergeMetadata(ctx, sessionMetadataPatchParams{
			OrganizationUUID: session.OrganizationUUID,
			WorkspaceUUID:    session.WorkspaceUUID, SessionExternalID: session.ExternalID,
			MetadataPatch: []byte(sessionPatch),
		})
		if err != nil {
			return err
		}
		if sessionRows == 0 {
			return ErrNotFound
		}

		workRows, err := workMapper.MergeMetadata(ctx, environmentWorkMetadataPatchParams{
			OrganizationUUID: work.OrganizationUUID, WorkspaceUUID: work.WorkspaceUUID,
			EnvironmentUUID: work.EnvironmentUUID, EnvironmentExternalID: work.EnvironmentExternalID,
			WorkExternalID: work.ExternalID, MetadataPatch: []byte(workPatch),
		})
		if err != nil {
			return err
		}
		if workRows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// TerminateManagedAgentCodeSession revokes credentials for a launch that did
// not complete. Repeating the operation is safe.
func (d *DB) TerminateManagedAgentCodeSession(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	codeSessionExternalID string,
) error {
	mapper := NewCodeSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.TerminateByExternalID(
		ctx,
		organizationUUID,
		workspaceUUID,
		codeSessionExternalID,
	)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
