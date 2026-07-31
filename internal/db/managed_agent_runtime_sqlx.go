package db

import (
	"context"
	"encoding/json"
)

const (
	bindManagedAgentSessionMetadataQuery = `
		update sessions
		set metadata = coalesce(metadata, CAST('{}' AS jsonb))
				|| CAST(:metadata_patch AS jsonb),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
	`
	bindManagedAgentWorkMetadataQuery = `
		update environment_work
		set metadata = coalesce(metadata, CAST('{}' AS jsonb))
				|| CAST(:metadata_patch AS jsonb),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
			and environment_uuid = :environment_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
	`
	terminateManagedAgentCodeSessionQuery = `
		update code_sessions
		set status = 'terminated',
			oauth_access_token_hash = null,
			worker_lease_expires_at = null,
			connection_status = 'disconnected',
			updated_at = now()
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
			and external_id = :code_session_external_id
			and deleted_at is null
	`
)

// BindManagedAgentRuntimeMetadata 将已启动的 Managed Agent runtime 信息同时发布到
// public Session 和调用方指定的 Environment Work。
//
// 当前 Runner 在 Environment Manager 后台启动命令成功提交后调用它。两个 patch
// 应是 JSON 对象；函数使用 PostgreSQL 的 jsonb 顶层合并保留已有的无关字段，并用
// patch 覆盖同名字段，同时刷新两条记录的 updated_at。函数本身不解析 patch，也不做
// 调用方鉴权；无效 JSON 会作为数据库错误返回，参数配对由受信任的 Runner 保证。
//
// Session 按 organization、workspace 和 session external ID 定位。Work 还会校验
// environment 的内部 ID、external ID 和 work external ID。已删除或不在这些范围内的
// 记录不会被更新，并返回 ErrNotFound。
//
// 例如：
//   - Session 已有 {"title":"demo"}，增加 runtime 和 code session ID；成功后
//     title 保留，两个目标都包含新的 runtime 信息。
//   - Session 存在，但 Work 已删除或 environment ID 不匹配；函数返回 ErrNotFound，
//     Session 上已经执行的更新也会回滚。
//   - 任一 patch 不是合法 JSON；数据库返回错误，两条记录都不会提交修改。
//
// 两次更新位于同一个 sqlx 事务中。UPDATE 会对命中的行加数据库行锁，但这里没有额外
// 的显式锁；并发调用会在相关行上串行执行，同名顶层字段由后执行的 patch 覆盖。成功时
// 返回 nil；查询、扫描或提交错误会原样返回。除 metadata 和 updated_at 外没有其他副作用。
func (d *DB) BindManagedAgentRuntimeMetadata(
	ctx context.Context,
	session Session,
	work EnvironmentWork,
	sessionPatch json.RawMessage,
	workPatch json.RawMessage,
) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sessionResult, err := namedExecContext(ctx, tx, bindManagedAgentSessionMetadataQuery, map[string]any{
		"organization_uuid":   session.OrganizationUUID,
		"workspace_uuid":      session.WorkspaceUUID,
		"session_external_id": session.ExternalID,
		"metadata_patch":      string(sessionPatch),
	})
	if err != nil {
		return err
	}
	sessionRows, err := sessionResult.RowsAffected()
	if err != nil {
		return err
	}
	if sessionRows == 0 {
		return ErrNotFound
	}

	workResult, err := namedExecContext(ctx, tx, bindManagedAgentWorkMetadataQuery, map[string]any{
		"organization_uuid":       work.OrganizationUUID,
		"workspace_uuid":          work.WorkspaceUUID,
		"environment_uuid":        work.EnvironmentUUID,
		"environment_external_id": work.EnvironmentExternalID,
		"work_external_id":        work.ExternalID,
		"metadata_patch":          string(workPatch),
	})
	if err != nil {
		return err
	}
	workRows, err := workResult.RowsAffected()
	if err != nil {
		return err
	}
	if workRows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// TerminateManagedAgentCodeSession revokes credentials for a launch that did
// not complete. Repeating the operation is safe.
func (d *DB) TerminateManagedAgentCodeSession(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	codeSessionExternalID string,
) error {
	result, err := namedExecContext(ctx, d.sql, terminateManagedAgentCodeSessionQuery, map[string]any{
		"organization_uuid":        organizationUUID,
		"workspace_uuid":           workspaceUUID,
		"code_session_external_id": codeSessionExternalID,
	})
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
