package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	filestoreFilesystemIDPrefix      = "claude_chat_"
	filestoreFilesystemIDMaxAttempts = 3
	filestoreWorkspaceExternalIDKey  = "filestore_filesystems_workspace_uuid_external_id_key"
	filestoreWorkspaceSessionKey     = "filestore_filesystems_workspace_session_active_v4_key"
)

// ResolveFilestoreTokenScope 用一次查询把 token 中的组织、账号、工作区与文件系统
// 绑定到同一条活跃租户链。当前模型没有 workspace alias，因此 tagged ID 与
// resolved tagged ID 都必须精确指向 workspace.external_id；日后引入 alias 时可在此处扩展解析。
func (d *DB) ResolveFilestoreTokenScope(
	ctx context.Context,
	organizationUUID string,
	accountUUID string,
	workspaceUUID string,
	workspaceTaggedID string,
	resolvedWorkspaceTaggedID string,
	filesystemID string,
) (FilestoreTokenScope, error) {
	// 查询末尾两列同时取回当前安全策略：组织 taints 来自 settings JSON，
	// CMEK 状态则由工作区是否配置 external_key_id 推导，供鉴权层校验 JWT 快照。
	return getFilestoreTokenScopeSQLX(ctx, d.sql, `
		select o.id as organization_id,
			cast(o.uuid as text) as organization_uuid,
			o.external_id as organization_external_id,
			w.id as workspace_id,
			cast(w.uuid as text) as workspace_uuid,
			w.external_id as workspace_external_id,
			u.id as account_id,
			cast(u.uuid as text) as account_uuid,
			u.external_id as account_external_id,
			fs.id as filesystem_id,
			cast(fs.uuid as text) as filesystem_uuid,
			fs.external_id as filesystem_external_id,
			coalesce(o.settings->'org_taints', cast('[]' as jsonb)) as org_taints_json,
			(nullif(trim(w.external_key_id), '') is not null) as workspace_cmek_enabled
		from organizations o
		join workspaces w
			on w.organization_id = o.id
		join users u
			on u.organization_id = o.id
			and u.deleted_at is null
		join filestore_filesystems fs
			on fs.organization_uuid = o.uuid
			and fs.workspace_uuid = w.uuid
			and fs.deleted_at is null
		join sessions s
			on s.uuid = fs.session_uuid
			and s.organization_id = o.id
			and s.workspace_id = w.id
			and s.archived_at is null
			and s.deleted_at is null
			and s.status <> 'terminated'
		where cast(o.uuid as text) = :organization_uuid
			and cast(u.uuid as text) = :account_uuid
			and cast(w.uuid as text) = :workspace_uuid
			and w.external_id = :workspace_tagged_id
			and w.external_id = :resolved_workspace_tagged_id
			and (
				fs.external_id = :filesystem_id
				or cast(fs.uuid as text) = lower(:filesystem_id)
			)
			and w.archived_at is null
		order by (fs.external_id = :filesystem_id) desc
		limit 1
	`, map[string]any{
		"organization_uuid":            strings.TrimSpace(organizationUUID),
		"account_uuid":                 strings.TrimSpace(accountUUID),
		"workspace_uuid":               strings.TrimSpace(workspaceUUID),
		"workspace_tagged_id":          strings.TrimSpace(workspaceTaggedID),
		"resolved_workspace_tagged_id": strings.TrimSpace(resolvedWorkspaceTaggedID),
		"filesystem_id":                strings.TrimSpace(filesystemID),
	})
}

// insertSessionFilesystemTx 在 public session 的创建事务中建立唯一命名空间。
// 随机 ID 的碰撞由数据库唯一约束裁决；只有该约束冲突时才重新取样。
func insertSessionFilesystemTx(ctx context.Context, tx pgx.Tx, session Session) (FilestoreFilesystem, error) {
	createdAt := filestoreNow(session.CreatedAt)
	return createFilestoreFilesystemWithGeneratedID(
		func() (string, error) {
			return ids.New(filestoreFilesystemIDPrefix)
		},
		func(externalID string) (FilestoreFilesystem, bool, error) {
			filesystem, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, `
			insert into filestore_filesystems (
				external_id, organization_uuid, workspace_uuid, session_uuid,
				code_session_uuid, created_by_api_key_uuid, created_at, updated_at
			)
			select $1, o.uuid, w.uuid, $2, null, ak.uuid, $6, $6
			from organizations o
			join workspaces w
				on w.id = $4
				and w.organization_id = o.id
			join api_keys ak
				on ak.id = $5
				and ak.workspace_id = w.id
			where o.id = $3
			on conflict on constraint filestore_filesystems_workspace_uuid_external_id_key do nothing
			returning `+filestoreFilesystemColumns()+`
		`, externalID, session.UUID, session.OrganizationID, session.WorkspaceID,
				session.CreatedByAPIKeyID, createdAt))
			if err == nil {
				return filesystem, true, nil
			}
			if isUniqueViolationOnConstraint(err, filestoreWorkspaceSessionKey) {
				return FilestoreFilesystem{}, false, ErrDuplicate
			}
			if !errors.Is(err, ErrNotFound) {
				return FilestoreFilesystem{}, false, err
			}

			var externalIDConflict bool
			if err := tx.QueryRow(ctx, `
			select exists (
				select 1
				from filestore_filesystems fs
				join workspaces w on w.uuid = fs.workspace_uuid
				where w.id = $1 and fs.external_id = $2
			)
		`, session.WorkspaceID, externalID).Scan(&externalIDConflict); err != nil {
				return FilestoreFilesystem{}, false, err
			}
			if !externalIDConflict {
				return FilestoreFilesystem{}, false, ErrPreconditionFailed
			}
			return FilestoreFilesystem{}, false, nil
		},
	)
}

func createFilestoreFilesystemWithGeneratedID(
	generateID func() (string, error),
	insert func(string) (FilestoreFilesystem, bool, error),
) (FilestoreFilesystem, error) {
	for range filestoreFilesystemIDMaxAttempts {
		externalID, err := generateID()
		if err != nil {
			return FilestoreFilesystem{}, err
		}
		filesystem, inserted, err := insert(externalID)
		if err != nil {
			return FilestoreFilesystem{}, err
		}
		if inserted {
			return filesystem, nil
		}
	}
	return FilestoreFilesystem{}, fmt.Errorf(
		"generate unique Filestore filesystem ID after %d attempts: %w",
		filestoreFilesystemIDMaxAttempts,
		ErrDuplicate,
	)
}

// ProvisionFilestoreFilesystem 在已验证的会话范围内幂等创建文件系统。
// 同一外部 ID 若已绑定其他会话则返回冲突，绝不静默改写归属。
func (d *DB) ProvisionFilestoreFilesystem(ctx context.Context, input ProvisionFilestoreFilesystemInput) (FilestoreFilesystem, bool, error) {
	if strings.TrimSpace(input.ExternalID) == "" {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	var err error
	input.OrganizationUUID, err = normalizeFilestoreReferenceUUID(input.OrganizationUUID)
	if err != nil {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	input.WorkspaceUUID, err = normalizeFilestoreReferenceUUID(input.WorkspaceUUID)
	if err != nil {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	input.SessionUUID, err = normalizeFilestoreReferenceUUID(input.SessionUUID)
	if err != nil {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	if input.CodeSessionUUID, err = normalizeOptionalFilestoreReferenceUUID(input.CodeSessionUUID); err != nil {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	if input.CreatedByAPIKeyUUID, err = normalizeOptionalFilestoreReferenceUUID(input.CreatedByAPIKeyUUID); err != nil {
		return FilestoreFilesystem{}, false, ErrPreconditionFailed
	}
	input.Now = filestoreNow(input.Now)

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return FilestoreFilesystem{}, false, err
	}
	defer tx.Rollback(ctx)

	// 按 (workspace, externalID) 串行化首次建档，避免并发请求各自通过“尚不存在”的检查。
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('filestore-provision:' || $1::text || ':' || $2::text, 0))`, input.WorkspaceUUID, input.ExternalID); err != nil {
		return FilestoreFilesystem{}, false, err
	}
	if err := validateFilestoreSessionBinding(ctx, tx, input); err != nil {
		return FilestoreFilesystem{}, false, err
	}

	existing, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = $1 and (external_id = $2 or uuid::text = lower($2)) and deleted_at is null
		order by (external_id = $2) desc
		limit 1
		for update
	`, input.WorkspaceUUID, input.ExternalID))
	if err == nil {
		if existing.OrganizationUUID != input.OrganizationUUID || existing.SessionUUID != input.SessionUUID {
			return FilestoreFilesystem{}, false, ErrDuplicate
		}
		if err := tx.Commit(ctx); err != nil {
			return FilestoreFilesystem{}, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return FilestoreFilesystem{}, false, err
	}
	if _, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = $1 and session_uuid = $2 and deleted_at is null
		limit 1
		for update
	`, input.WorkspaceUUID, input.SessionUUID)); err == nil {
		return FilestoreFilesystem{}, false, ErrDuplicate
	} else if !errors.Is(err, ErrNotFound) {
		return FilestoreFilesystem{}, false, err
	}

	filesystem, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, `
		insert into filestore_filesystems (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			code_session_uuid, created_by_api_key_uuid, created_at, updated_at
		)
		values (coalesce(nullif($1, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, $7, $8, $8)
		returning `+filestoreFilesystemColumns()+`
	`, input.UUID, input.ExternalID, input.OrganizationUUID, input.WorkspaceUUID, input.SessionUUID,
		input.CodeSessionUUID, input.CreatedByAPIKeyUUID, input.Now))
	if isUniqueViolationOnConstraint(err, filestoreWorkspaceSessionKey) ||
		isUniqueViolationOnConstraint(err, filestoreWorkspaceExternalIDKey) {
		return FilestoreFilesystem{}, false, ErrDuplicate
	}
	if err != nil {
		return FilestoreFilesystem{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FilestoreFilesystem{}, false, err
	}
	return filesystem, true, nil
}

// GetFilestoreFilesystem 在工作区边界内按外部 ID 或 UUID 查找文件系统。
func (d *DB) GetFilestoreFilesystem(ctx context.Context, workspaceID int64, externalID string) (FilestoreFilesystem, error) {
	return getFilestoreFilesystemSQLX(ctx, d.sql, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and (
				external_id = :filesystem_id
				or cast(uuid as text) = lower(:filesystem_id)
			)
			and deleted_at is null
		order by (external_id = :filesystem_id) desc
		limit 1
	`, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": externalID,
	})
}

// GetFilestoreFilesystemBySession 返回 public session 唯一拥有的活动文件系统。
// Code session 是可重建的执行实例，不参与文件系统归属判断。
func (d *DB) GetFilestoreFilesystemBySession(ctx context.Context, workspaceID int64, sessionExternalID string) (FilestoreFilesystem, error) {
	return getFilestoreFilesystemSQLX(ctx, d.sql, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and session_uuid = (
				select uuid from sessions
				where workspace_id = :workspace_id
					and external_id = :session_external_id
					and deleted_at is null
			)
			and deleted_at is null
		limit 1
	`, map[string]any{
		"workspace_id":        workspaceID,
		"session_external_id": sessionExternalID,
	})
}

// retireSessionFilesystemTx 先撤销命名空间访问，再投递有界的后台回收任务。
// 文件元数据和 S3 对象都由 worker 分批处理，Session 删除事务不会随文件数量增长。
func retireSessionFilesystemTx(ctx context.Context, tx pgx.Tx, session Session) error {
	retiredAt := filestoreNow(session.UpdatedAt)
	filesystem, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, `
		with retired as (
			update filestore_filesystems fs
			set deleted_at = coalesce(fs.deleted_at, $3), updated_at = $3
			from workspaces w, organizations o
			where w.id = $1
				and o.id = $2
				and w.organization_id = o.id
				and fs.workspace_uuid = w.uuid
				and fs.organization_uuid = o.uuid
				and fs.session_uuid = $4
				and fs.deleted_at is null
			returning fs.id
		)
		select `+filestoreFilesystemColumns()+`
		from filestore_filesystems
		where id = (select id from retired)
	`, session.WorkspaceID, session.OrganizationID, retiredAt, session.UUID))
	if errors.Is(err, ErrNotFound) {
		// 兼容自动建档上线前已经存在、但尚未回填 filesystem 的历史会话。
		return nil
	}
	if err != nil {
		return err
	}
	_, err = enqueueFilestoreFilesystemCleanupJobTx(ctx, tx, filesystem, session.WorkspaceID, retiredAt)
	return err
}

func validateFilestoreSessionBinding(ctx context.Context, tx pgx.Tx, input ProvisionFilestoreFilesystemInput) error {
	var valid bool
	err := tx.QueryRow(ctx, `
		select exists (
			select 1
			from sessions s
			join workspaces w on w.id = s.workspace_id and w.organization_id = s.organization_id
			join organizations o on o.id = s.organization_id
			where s.uuid = $1
				and o.uuid = $2 and w.uuid = $3
				and s.status <> 'terminated' and s.archived_at is null and s.deleted_at is null
				and w.archived_at is null
		)
	`, input.SessionUUID, input.OrganizationUUID, input.WorkspaceUUID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrNotFound
	}
	if input.CodeSessionUUID != nil {
		err = tx.QueryRow(ctx, `
				select exists (
					select 1
					from code_sessions cs
					join sessions s on s.id = cs.session_id
					join workspaces w on w.id = cs.workspace_id and w.organization_id = cs.organization_id
					join organizations o on o.id = cs.organization_id
					where cs.uuid = $1
						and o.uuid = $2 and w.uuid = $3
						and s.uuid = $4
						and cs.status = 'active' and cs.deleted_at is null
				)
			`, *input.CodeSessionUUID, input.OrganizationUUID, input.WorkspaceUUID, input.SessionUUID).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrNotFound
		}
	}
	if input.CreatedByAPIKeyUUID != nil {
		err = tx.QueryRow(ctx, `
				select exists (
					select 1
					from api_keys ak
					join workspaces w on w.id = ak.workspace_id
					where ak.uuid = $1 and w.uuid = $2
				)
			`, *input.CreatedByAPIKeyUUID, input.WorkspaceUUID).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrNotFound
		}
	}
	return nil
}

func normalizeFilestoreReferenceUUID(value string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func normalizeOptionalFilestoreReferenceUUID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeFilestoreReferenceUUID(*value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}
