package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
)

const (
	filestoreFilesystemIDPrefix      = "claude_chat_"
	filestoreFilesystemIDMaxAttempts = 3
	filestoreWorkspaceExternalIDKey  = "filestore_filesystems_workspace_uuid_external_id_key"
	filestoreWorkspaceSessionKey     = "filestore_filesystems_workspace_session_active_v4_key"
)

var (
	provisionFilestoreAdvisoryLockQuery = `
		select pg_advisory_xact_lock(
			hashtextextended(
				concat(
					'filestore-provision',
					chr(58),
					CAST(:workspace_uuid AS text),
					chr(58),
					CAST(:filesystem_external_id AS text)
				),
				0
			)
		)
	`
	validateFilestoreSessionBindingQuery = `
		select w.id as workspace_id
		from sessions s
		join workspaces w
			on w.id = s.workspace_id
			and w.organization_id = s.organization_id
		join organizations o
			on o.id = s.organization_id
		where s.uuid = CAST(:session_uuid AS uuid)
			and o.uuid = CAST(:organization_uuid AS uuid)
			and w.uuid = CAST(:workspace_uuid AS uuid)
			and s.status <> 'terminated'
			and s.archived_at is null
			and s.deleted_at is null
			and w.archived_at is null
			and (
				CAST(:code_session_uuid AS uuid) is null
				or exists (
					select 1
					from code_sessions cs
					where cs.uuid = CAST(:code_session_uuid AS uuid)
						and cs.session_id = s.id
						and cs.organization_id = s.organization_id
						and cs.workspace_id = s.workspace_id
						and cs.status = 'active'
						and cs.deleted_at is null
				)
			)
			and (
				CAST(:created_by_api_key_uuid AS uuid) is null
				or exists (
					select 1
					from api_keys ak
					where ak.uuid = CAST(:created_by_api_key_uuid AS uuid)
						and ak.workspace_id = s.workspace_id
				)
			)
		limit 1
		for update of s
	`
	provisionFilestoreWorkspaceLockQuery = `
		select pg_advisory_xact_lock(:workspace_id)
	`
	provisionFilestoreByExternalIDQuery = `
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and (
				external_id = :filesystem_external_id
				or CAST(uuid AS text) = lower(:filesystem_external_id)
			)
			and deleted_at is null
		order by (external_id = :filesystem_external_id) desc
		limit 1
		for update
	`
	provisionFilestoreBySessionQuery = `
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and deleted_at is null
		limit 1
		for update
	`
	insertProvisionedFilestoreQuery = `
		insert into filestore_filesystems (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			code_session_uuid, created_by_api_key_uuid, created_at, updated_at
		)
		values (
			coalesce(CAST(nullif(:filesystem_uuid, '') AS uuid), gen_random_uuid()),
			:filesystem_external_id,
			CAST(:organization_uuid AS uuid),
			CAST(:workspace_uuid AS uuid),
			CAST(:session_uuid AS uuid),
			CAST(:code_session_uuid AS uuid),
			CAST(:created_by_api_key_uuid AS uuid),
			:now,
			:now
		)
		returning ` + filestoreFilesystemColumns() + `
	`
	provisionFilestoreNamespaceLockQuery = `
		select pg_advisory_xact_lock(-CAST(:filesystem_id AS bigint))
	`
)

type filestoreSessionBindingRow struct {
	WorkspaceID int64 `db:"workspace_id"`
}

const filestoreSessionTokenScopeQuery = `
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
	from sessions s
	join organizations o
		on o.id = s.organization_id
	join workspaces w
		on w.id = s.workspace_id
		and w.organization_id = s.organization_id
	join api_keys ak
		on ak.id = s.created_by_api_key_id
		and ak.workspace_id = s.workspace_id
	join users u
		on u.id = ak.created_by_user_id
		and u.organization_id = s.organization_id
		and u.deleted_at is null
	join filestore_filesystems fs
		on fs.organization_uuid = o.uuid
		and fs.workspace_uuid = w.uuid
		and fs.session_uuid = s.uuid
		and fs.deleted_at is null
	where s.workspace_id = :workspace_id
		and s.external_id = :session_external_id
		and s.status <> 'terminated'
		and s.archived_at is null
		and s.deleted_at is null
		and w.archived_at is null
	limit 1
`

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

// ProvisionFilestoreFilesystem 为一个活动 Session 幂等建立 Filestore filesystem。
//
// 它先检查 external ID 和各个引用 UUID，再从数据库确认 Organization、Workspace、
// Session、可选 Code Session 和可选 API key 的归属关系。Session 和 Workspace
// 必须仍然有效。校验通过后，函数会复用已有 filesystem 或创建新记录，并确保
// /outputs、/uploads、/transcripts 和 /tool_results 四个固定根目录存在。
//
// 例如：
//   - Session 尚无 filesystem：创建 filesystem 和四个根目录，返回 filesystem、
//     true、nil。
//   - 使用相同 external ID 和 Session 重试：复用原记录并补齐可能缺失的根目录，
//     返回 filesystem、false、nil。false 只表示本次没有新建 filesystem 记录。
//   - 同一 Workspace 内的 external ID 已属于其他 Session，或当前 Session 已有
//     另一个活动 filesystem：返回 ErrDuplicate，不会改写已有归属。
//
// 整个过程在同一个 sqlx 事务中完成。函数组合使用 external ID advisory lock、
// Session 行锁、Workspace advisory lock 和 filesystem namespace advisory lock，
// 并由数据库唯一索引兜底，避免并发请求创建重复记录，也避免与 Session 删除或目录
// 更新发生竞态。任何一步失败都会回滚，不会留下只有 filesystem、没有完整固定
// 根目录的状态。
//
// 输入缺失或引用 UUID 非法时返回 ErrPreconditionFailed；归属链不存在或已失效时
// 返回 ErrNotFound；名称或 Session 归属冲突时返回 ErrDuplicate。固定根路径被活动
// 文件占用时会返回 ErrFilestorePathExists，其他查询、写入或提交错误原样返回。
// 这里校验的是数据库归属和生命周期，不负责调用方的 API 权限授权。
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

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return FilestoreFilesystem{}, false, err
	}
	defer tx.Rollback()

	// 按 (workspace, externalID) 串行化首次建档，避免并发请求各自通过“尚不存在”的检查。
	arguments := provisionFilestoreFilesystemArguments(input)
	if _, err := namedExecContext(ctx, tx, provisionFilestoreAdvisoryLockQuery, arguments); err != nil {
		return FilestoreFilesystem{}, false, err
	}
	binding, err := validateFilestoreSessionBinding(ctx, tx, input)
	if err != nil {
		return FilestoreFilesystem{}, false, err
	}
	arguments["workspace_id"] = binding.WorkspaceID
	if _, err := namedExecContext(ctx, tx, provisionFilestoreWorkspaceLockQuery, arguments); err != nil {
		return FilestoreFilesystem{}, false, err
	}

	existing, err := getFilestoreFilesystemSQLX(
		ctx,
		tx,
		filestoreFilesystemSelectSQL()+provisionFilestoreByExternalIDQuery,
		arguments,
	)
	if err == nil {
		if existing.OrganizationUUID != input.OrganizationUUID || existing.SessionUUID != input.SessionUUID {
			return FilestoreFilesystem{}, false, ErrDuplicate
		}
		if err := ensureProvisionedFilestoreRootsTx(ctx, tx, binding.WorkspaceID, existing, input.Now); err != nil {
			return FilestoreFilesystem{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return FilestoreFilesystem{}, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return FilestoreFilesystem{}, false, err
	}
	if _, err := getFilestoreFilesystemSQLX(
		ctx,
		tx,
		filestoreFilesystemSelectSQL()+provisionFilestoreBySessionQuery,
		arguments,
	); err == nil {
		return FilestoreFilesystem{}, false, ErrDuplicate
	} else if !errors.Is(err, ErrNotFound) {
		return FilestoreFilesystem{}, false, err
	}

	filesystem, err := getFilestoreFilesystemSQLX(
		ctx,
		tx,
		insertProvisionedFilestoreQuery,
		arguments,
	)
	if isUniqueViolationOnConstraint(err, filestoreWorkspaceSessionKey) ||
		isUniqueViolationOnConstraint(err, filestoreWorkspaceExternalIDKey) {
		return FilestoreFilesystem{}, false, ErrDuplicate
	}
	if err != nil {
		return FilestoreFilesystem{}, false, err
	}
	if err := ensureProvisionedFilestoreRootsTx(ctx, tx, binding.WorkspaceID, filesystem, input.Now); err != nil {
		return FilestoreFilesystem{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreFilesystem{}, false, err
	}
	return filesystem, true, nil
}

func provisionFilestoreFilesystemArguments(input ProvisionFilestoreFilesystemInput) map[string]any {
	return map[string]any{
		"filesystem_uuid":         input.UUID,
		"filesystem_external_id":  input.ExternalID,
		"organization_uuid":       input.OrganizationUUID,
		"workspace_uuid":          input.WorkspaceUUID,
		"session_uuid":            input.SessionUUID,
		"code_session_uuid":       input.CodeSessionUUID,
		"created_by_api_key_uuid": input.CreatedByAPIKeyUUID,
		"now":                     input.Now,
	}
}

func ensureProvisionedFilestoreRootsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	filesystem FilestoreFilesystem,
	now time.Time,
) error {
	if _, err := namedExecContext(ctx, tx, provisionFilestoreNamespaceLockQuery, map[string]any{
		"filesystem_id": filesystem.ID,
	}); err != nil {
		return err
	}
	return ensureFilestoreFixedRootsTx(ctx, tx, workspaceID, filesystem, now)
}

// ensureFilestoreFixedRootsTx 在指定 filesystem 的数据库命名空间中确保四个固定根目录存在。
//
// 固定根目录是 /outputs、/uploads、/transcripts 和 /tool_results。rclone 会把这些
// 路径挂载到 Sandbox，因此目录必须先存在于 Filestore 数据库中。每个路径都通过
// ensureFilestoreDirectoryTx 幂等处理：已有目录保持不变，缺失目录会被创建；如果
// 路径上是已过期文件，则会释放旧文件的存储归属并把该 entry 改成目录；其中由
// Filestore 拥有的对象还会进入清理队列。
//
// 例如：
//   - 新 filesystem 中还没有任何 entry：创建四个目录并返回 nil。
//   - 四个目录已经存在：不重复插入，直接返回 nil。
//   - /uploads 被一个未过期文件占用：返回 ErrFilestorePathExists，避免 rclone 把
//     文件路径当成目录挂载。
//
// 所有改动都使用调用方传入的同一个事务。任一路径处理失败时立即返回错误，调用方
// 应回滚事务，避免提交只创建了一部分根目录的状态。该函数本身不加锁，也不提交或
// 回滚事务：新建 Session 时 filesystem 尚未对其他事务可见；对已有 filesystem
// 补建根目录时，调用方必须先持有 namespace advisory lock。
func ensureFilestoreFixedRootsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	filesystem FilestoreFilesystem,
	now time.Time,
) error {
	now = filestoreNow(now)
	for _, rootPath := range filestoreFixedRootPaths {
		if _, err := ensureFilestoreDirectoryTx(
			ctx,
			tx,
			workspaceID,
			filesystem,
			rootPath,
			now,
		); err != nil {
			return err
		}
	}
	return nil
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

// GetFilestoreTokenScopeForSessionIssue 为 Runner 查询签发 Filestore token 所需的
// 可信身份和授权范围。
//
// 它从指定 Workspace 中查找 Active Session，并确认 Session、Organization、Workspace、
// 创建 Session 的 API key 和用户，以及 Filestore filesystem 属于同一条有效的归属链。
// Session 必须未终止、未归档、未删除，Workspace 必须未归档，用户和 filesystem
// 也必须仍然有效。查询结果还包含当前的组织 taints 和 Workspace CMEK 状态，供
// Runner 写入 token；这些安全字段来自数据库，不接受客户端提供的值。
//
// 例如：
//   - session_A 及其创建用户、Workspace 和 filesystem 都有效：返回完整的
//     FilestoreTokenScope，Runner 可以据此签发读写 token 和只读 token。
//   - session_A 实际属于 workspace_A，但调用方传入 workspace_B，或者 Session
//     已终止：返回 ErrNotFound，Runner 不会签发 token。
//   - organizations.settings 中的 org_taints 无法解析为字符串数组：返回解析错误，
//     防止使用不完整或错误的安全策略签发 token。
//
// 这是只读查询，不开启事务、不加锁，也不会修改数据库。查询不到完整有效的归属链
// 时返回 ErrNotFound；数据库查询失败或策略字段解析失败时返回对应错误。签发后的
// token 仍会在每次 Filestore 请求中重新回查数据库，因此这里返回的是签发时快照，
// 不是绕过后续鉴权的永久授权。
func (d *DB) GetFilestoreTokenScopeForSessionIssue(ctx context.Context, workspaceID int64, sessionExternalID string) (FilestoreTokenScope, error) {
	return getFilestoreTokenScopeSQLX(ctx, d.sql, filestoreSessionTokenScopeQuery, filestoreSessionTokenScopeArguments(workspaceID, sessionExternalID))
}

func filestoreSessionTokenScopeArguments(workspaceID int64, sessionExternalID string) map[string]any {
	return map[string]any{
		"workspace_id":        workspaceID,
		"session_external_id": strings.TrimSpace(sessionExternalID),
	}
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

// validateFilestoreSessionBinding 确认创建 Filestore filesystem 时传入的各个 UUID
// 确实属于同一个可用的 Session。
//
// 它会检查：
//   - organization、workspace 和 session 的归属关系一致；
//   - Session 未终止、未归档、未删除，Workspace 未归档；
//   - 可选的 code session 属于该 Session，且仍处于 active 状态；
//   - 可选的创建者 API key 属于该 Workspace。
//
// 例如：
//   - session_A 属于 workspace_A 和 organization_A，附带的 code_session_A 也属于
//     session_A：校验通过并返回 workspace_A 的内部 ID。
//   - session_A 实际属于 workspace_A，但调用方传入 workspace_B：返回 ErrNotFound。
//   - code_session_B 属于另一个 Session，或已经失效：返回 ErrNotFound。
//
// 该函数只检查数据库归属关系和资源状态，不负责 API 权限授权。任何一项检查失败
// 都统一返回 ErrNotFound。校验成功后，查询会通过 SELECT ... FOR UPDATE 锁定
// Session 行，避免 filesystem 建档期间该 Session 被并发修改；返回的 Workspace
// 内部 ID 用于获取后续的 Workspace 级事务锁。
func validateFilestoreSessionBinding(
	ctx context.Context,
	tx *sqlx.Tx,
	input ProvisionFilestoreFilesystemInput,
) (filestoreSessionBindingRow, error) {
	var binding filestoreSessionBindingRow
	err := namedGetContext(
		ctx,
		tx,
		&binding,
		validateFilestoreSessionBindingQuery,
		provisionFilestoreFilesystemArguments(input),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return filestoreSessionBindingRow{}, ErrNotFound
	}
	if err != nil {
		return filestoreSessionBindingRow{}, err
	}
	return binding, nil
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
