package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/jackc/pgx/v5"
)

const (
	mcpToolCatalogReferenceTouchInterval = 5 * time.Minute
)

// MCPToolCatalog 是按规范化 transport_type + endpoint_url 全局共享的匿名派生缓存，不属于任何组织或 workspace。
// 这里不得存放认证信息或租户特有的运行时观察；此类数据必须使用单独的租户级模型。
type MCPToolCatalog struct {
	ID                  int64
	ExternalID          string
	TransportType       string
	EndpointURL         string
	Tools               json.RawMessage
	Source              *string
	LastResultStatus    *string
	ProtocolVersion     *string
	ServerInfo          json.RawMessage
	CatalogHash         *string
	DiscoveredAt        *time.Time
	ExpiresAt           *time.Time
	LastAttemptAt       *time.Time
	LastErrorCode       *string
	LastErrorMessage    *string
	LastErrorAt         *time.Time
	RetryAfter          *time.Time
	RequestedGeneration int64
	SettledGeneration   int64
	LastReferencedAt    time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type EnsureMCPToolCatalogInput struct {
	// JobWorkspaceExternalID 是便于日志与排障识别的稳定业务 ID，只记录任务来源，不参与全局 catalog identity。
	JobWorkspaceExternalID string
	TransportType          string
	EndpointURL            string
	Trigger                string
	Force                  bool
	Now                    time.Time
}

type EnsureMCPToolCatalogResult struct {
	Catalog MCPToolCatalog
	Queued  bool
}

type MCPToolDiscoveryJob struct {
	ID                int64
	Attempts          int
	CatalogExternalID string
	EndpointURL       string
	Generation        int64
}

type CompleteMCPToolDiscoveryInput struct {
	JobID             int64
	WorkerID          string
	CatalogExternalID string
	Generation        int64
	Tools             json.RawMessage
	ProtocolVersion   string
	ServerInfo        json.RawMessage
	CatalogHash       string
	DiscoveredAt      time.Time
	ExpiresAt         time.Time
}

type FailMCPToolDiscoveryInput struct {
	JobID             int64
	WorkerID          string
	CatalogExternalID string
	Generation        int64
	Attempts          int
	MaxAttempts       int
	Retryable         bool
	RetryDelay        time.Duration
	ErrorCode         string
	ErrorMessage      string
	Now               time.Time
}

func (d *DB) EnsureMCPToolCatalog(ctx context.Context, input EnsureMCPToolCatalogInput) (EnsureMCPToolCatalogResult, error) {
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if input.TransportType == "" {
		input.TransportType = "url"
	}
	input.JobWorkspaceExternalID = strings.TrimSpace(input.JobWorkspaceExternalID)
	if input.JobWorkspaceExternalID == "" {
		return EnsureMCPToolCatalogResult{}, fmt.Errorf("job workspace external id is required")
	}
	// Agent 详情页会高频调用 Ensure；引用时间仍在窗口内且 catalog 可复用时直接只读返回，
	// 避免每次轮询都更新 last_referenced_at/updated_at。执行中的 generation 即使 Force=true 也复用；
	// Force 只跳过已完成 catalog 的 fresh/backoff 限制，不会制造并行探测。
	if existing, getErr := d.GetMCPToolCatalog(ctx, input.TransportType, input.EndpointURL); getErr == nil {
		referenceIsRecent := !existing.LastReferencedAt.Before(input.Now.Add(-mcpToolCatalogReferenceTouchInterval))
		if referenceIsRecent && canReuseMCPToolCatalog(existing, input) {
			return EnsureMCPToolCatalogResult{Catalog: existing}, nil
		}
	} else if !errors.Is(getErr, ErrNotFound) {
		return EnsureMCPToolCatalogResult{}, getErr
	}
	externalID, err := ids.New("mcpc_")
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	catalog, err := scanMCPToolCatalog(tx.QueryRow(ctx, `
		insert into mcp_tool_catalogs (
			external_id, transport_type, endpoint_url, last_referenced_at, created_at, updated_at
		)
		values ($1, $2, $3, $4, $4, $4)
		on conflict (transport_type, endpoint_url)
		do update set last_referenced_at = excluded.last_referenced_at,
			updated_at = excluded.updated_at
		returning `+mcpToolCatalogColumns()+`
	`, externalID, input.TransportType, input.EndpointURL, input.Now))
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}

	if canReuseMCPToolCatalog(catalog, input) {
		if err := tx.Commit(ctx); err != nil {
			return EnsureMCPToolCatalogResult{}, err
		}
		return EnsureMCPToolCatalogResult{Catalog: catalog}, nil
	}

	// MCP 业务链只传递可读的 workspace external ID；直到写入通用 jobs 表时，
	// 才在同一事务内解析其 bigint 主键。该内部主键仍不参与 catalog 的全局去重。
	var jobWorkspaceID int64
	if err := tx.QueryRow(ctx, `
		select id
		from workspaces
		where external_id = $1
	`, input.JobWorkspaceExternalID).Scan(&jobWorkspaceID); err != nil {
		return EnsureMCPToolCatalogResult{}, mapNoRows(err)
	}

	// generation 递增和对应 job 写入必须在同一事务中，避免“已标记刷新但没有任务”。
	// job external_id 由 catalog 与 generation 确定，使并发 Ensure 可以安全幂等。
	generation := catalog.RequestedGeneration + 1
	catalog, err = scanMCPToolCatalog(tx.QueryRow(ctx, `
		update mcp_tool_catalogs
		set requested_generation = $2,
			last_referenced_at = $3,
			updated_at = $3
		where id = $1
		returning `+mcpToolCatalogColumns()+`
	`, catalog.ID, generation, input.Now))
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"schema_version":        1,
		"catalog_external_id":   catalog.ExternalID,
		"generation":            generation,
		"trigger":               input.Trigger,
		"workspace_external_id": input.JobWorkspaceExternalID,
	})
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	jobExternalID := mcpToolDiscoveryJobExternalID(catalog.ExternalID, generation)
	tag, err := tx.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload, run_after, created_at, updated_at)
		values ($1, $2, 'mcp_tool_discovery', 'pending', $3::jsonb, $4, $4, $4)
		on conflict (external_id) do nothing
	`, jobExternalID, jobWorkspaceID, jsonArg(payload), input.Now)
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	return EnsureMCPToolCatalogResult{Catalog: catalog, Queued: tag.RowsAffected() > 0}, nil
}

func canReuseMCPToolCatalog(catalog MCPToolCatalog, input EnsureMCPToolCatalogInput) bool {
	active := catalog.RequestedGeneration > catalog.SettledGeneration
	fresh := catalog.Tools != nil && catalog.ExpiresAt != nil && catalog.ExpiresAt.After(input.Now)
	backingOff := catalog.RetryAfter != nil && catalog.RetryAfter.After(input.Now)
	return active || (!input.Force && (fresh || backingOff))
}

func (d *DB) GetMCPToolCatalog(ctx context.Context, transportType, endpointURL string) (MCPToolCatalog, error) {
	return scanMCPToolCatalog(d.Pool.QueryRow(ctx, `
		select `+mcpToolCatalogColumns()+`
		from mcp_tool_catalogs
		where transport_type = $1
			and endpoint_url = $2
	`, transportType, endpointURL))
}

func (d *DB) LeaseMCPToolDiscoveryJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]MCPToolDiscoveryJob, error) {
	if limit <= 0 {
		limit = 3
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	// 多个 worker 实例通过 FOR UPDATE SKIP LOCKED 并发领取任务；过期的 running lease 可被重新领取。
	// 完成或失败时还会校验 locked_by，防止旧 worker 在 lease 失效后结算任务。
	rows, err := d.Pool.Query(ctx, `
		with next_jobs as (
			select id
			from jobs
			where type = 'mcp_tool_discovery'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit $1
			for update skip locked
		), leased as (
			update jobs j
			set status = 'running',
				locked_by = $2,
				locked_until = now() + $3::interval,
				updated_at = now()
			from next_jobs
			where j.id = next_jobs.id
			returning j.id, j.payload, j.attempts
		)
		select l.id, l.attempts,
			c.external_id, c.endpoint_url,
			coalesce((l.payload->>'generation')::bigint, 0)
		from leased l
		join mcp_tool_catalogs c
			on c.external_id = l.payload->>'catalog_external_id'
	`, limit, workerID, leaseDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []MCPToolDiscoveryJob
	for rows.Next() {
		var job MCPToolDiscoveryJob
		if err := rows.Scan(
			&job.ID, &job.Attempts,
			&job.CatalogExternalID, &job.EndpointURL, &job.Generation,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) CompleteMCPToolDiscovery(ctx context.Context, input CompleteMCPToolDiscoveryInput) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 先确认 job 仍由当前 worker 持有，再通过 requested_generation 对 catalog 做 CAS；
	// lease 已过期或旧 generation 的 worker 因而不能覆盖更新一代的结果。
	if err := settleMCPToolDiscoveryJob(ctx, tx, input.JobID, input.WorkerID, "completed", 0, time.Time{}, "", time.Time{}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		update mcp_tool_catalogs
		set tools = $3::jsonb,
			source = 'anonymous_probe',
			last_result_status = 'success',
			protocol_version = nullif($4, ''),
			server_info = $5::jsonb,
			catalog_hash = nullif($6, ''),
			discovered_at = $7,
			expires_at = $8,
			last_attempt_at = $7,
			last_error_code = null,
			last_error_message = null,
			last_error_at = null,
			retry_after = null,
			settled_generation = $2,
			updated_at = $7
		where external_id = $1
			and requested_generation = $2
			and settled_generation < $2
	`, input.CatalogExternalID, input.Generation,
		jsonArg(input.Tools), input.ProtocolVersion, nullableJSONArg(input.ServerInfo), input.CatalogHash,
		input.DiscoveredAt, input.ExpiresAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) FailMCPToolDiscovery(ctx context.Context, input FailMCPToolDiscoveryInput) error {
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = 4
	}
	nextAttempts := input.Attempts + 1
	willRetry := input.Retryable && nextAttempts < input.MaxAttempts

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 可重试且次数未耗尽时只延后 job，generation 保持 active，并继续保留上一份成功快照。
	// 只有终态错误或重试耗尽才结算 generation，避免等待重试期间又创建并行探测。
	if willRetry {
		runAfter := input.Now.Add(input.RetryDelay)
		if err := settleMCPToolDiscoveryJob(ctx, tx, input.JobID, input.WorkerID, "retry", nextAttempts, runAfter, input.ErrorCode, input.Now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			update mcp_tool_catalogs
			set last_attempt_at = $3,
				updated_at = $3
			where external_id = $1
				and requested_generation = $2
		`, input.CatalogExternalID, input.Generation, input.Now)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// 非重试错误表示发现流程已得到业务终态（例如 auth_required），因此 job 记为 completed；
	// 只有可重试错误耗尽尝试次数才记为 failed。
	jobStatus := "completed"
	if input.Retryable {
		jobStatus = "failed"
	}
	if err := settleMCPToolDiscoveryJob(ctx, tx, input.JobID, input.WorkerID, jobStatus, nextAttempts, time.Time{}, input.ErrorCode, input.Now); err != nil {
		return err
	}
	resultStatus := "error"
	if input.ErrorCode == "auth_required" {
		resultStatus = "auth_required"
	}
	retryAfter := input.Now.Add(input.RetryDelay)
	_, err = tx.Exec(ctx, `
		update mcp_tool_catalogs
		set last_result_status = $3,
			last_attempt_at = $4,
			last_error_code = $5,
			last_error_message = nullif($6, ''),
			last_error_at = $4,
			retry_after = $7,
			settled_generation = $2,
			updated_at = $4
		where external_id = $1
			and requested_generation = $2
			and settled_generation < $2
	`, input.CatalogExternalID, input.Generation,
		resultStatus, input.Now, input.ErrorCode, truncateDatabaseText(input.ErrorMessage, 512), retryAfter)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) RunMCPToolCatalogRetention(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 保留策略分阶段执行：7 天后清除错误详情，30 天后清除陈旧工具载荷；
	// 工具载荷与整行删除只处理不存在 active generation 的记录。项目不使用外键，
	// 因此先删除终态 job，再删除 catalog，最后兜底清理孤儿 job。
	if _, err := tx.Exec(ctx, `
		update mcp_tool_catalogs
		set last_error_message = null,
			updated_at = $1::timestamptz
		where last_error_message is not null
			and last_error_at < $1::timestamptz - interval '7 days'
	`, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update mcp_tool_catalogs
		set tools = null,
			server_info = null,
			catalog_hash = null,
			source = null,
			discovered_at = null,
			expires_at = null,
			updated_at = $1::timestamptz
		where tools is not null
			and discovered_at < $1::timestamptz - interval '30 days'
			and requested_generation = settled_generation
	`, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		delete from jobs j
		using mcp_tool_catalogs c
		where j.type = 'mcp_tool_discovery'
			and j.payload->>'catalog_external_id' = c.external_id
			and j.status in ('completed', 'failed')
			and c.last_referenced_at < $1::timestamptz - interval '30 days'
			and c.requested_generation = c.settled_generation
	`, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		delete from mcp_tool_catalogs
		where last_referenced_at < $1::timestamptz - interval '30 days'
			and requested_generation = settled_generation
	`, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		delete from jobs j
		where j.type = 'mcp_tool_discovery'
			and j.status in ('completed', 'failed')
			and not exists (
				select 1 from mcp_tool_catalogs c
				where c.external_id = j.payload->>'catalog_external_id'
			)
	`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func settleMCPToolDiscoveryJob(
	ctx context.Context,
	tx pgx.Tx,
	jobID int64,
	workerID string,
	status string,
	attempts int,
	runAfter time.Time,
	errorCode string,
	now time.Time,
) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if runAfter.IsZero() {
		runAfter = now
	}
	tag, err := tx.Exec(ctx, `
		update jobs
		set status = $3,
			attempts = case when $4 > 0 then $4 else attempts end,
			run_after = $5,
			locked_by = null,
			locked_until = null,
			updated_at = $7::timestamptz,
			payload = case
				when $6 = '' then payload
				else payload || jsonb_build_object('last_error_code', $6::text, 'last_error_at', ($7::timestamptz)::text)
			end
		where id = $1
			and type = 'mcp_tool_discovery'
			and status = 'running'
			and locked_by = $2
	`, jobID, workerID, status, attempts, runAfter, errorCode, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func mcpToolCatalogColumns() string {
	return `id, external_id, transport_type, endpoint_url, tools, source,
		last_result_status, protocol_version, server_info, catalog_hash,
		discovered_at, expires_at, last_attempt_at, last_error_code, last_error_message,
		last_error_at, retry_after, requested_generation, settled_generation,
		last_referenced_at, created_at, updated_at`
}

type mcpCatalogRowScanner interface {
	Scan(dest ...any) error
}

func scanMCPToolCatalog(row mcpCatalogRowScanner) (MCPToolCatalog, error) {
	var catalog MCPToolCatalog
	var tools, serverInfo []byte
	err := row.Scan(
		&catalog.ID, &catalog.ExternalID, &catalog.TransportType, &catalog.EndpointURL,
		&tools, &catalog.Source, &catalog.LastResultStatus,
		&catalog.ProtocolVersion, &serverInfo, &catalog.CatalogHash, &catalog.DiscoveredAt,
		&catalog.ExpiresAt, &catalog.LastAttemptAt, &catalog.LastErrorCode,
		&catalog.LastErrorMessage, &catalog.LastErrorAt, &catalog.RetryAfter,
		&catalog.RequestedGeneration, &catalog.SettledGeneration, &catalog.LastReferencedAt,
		&catalog.CreatedAt, &catalog.UpdatedAt,
	)
	if err != nil {
		return MCPToolCatalog{}, mapNoRows(err)
	}
	catalog.Tools = copyRaw(tools)
	catalog.ServerInfo = copyRaw(serverInfo)
	return catalog, nil
}

func mcpToolDiscoveryJobExternalID(catalogExternalID string, generation int64) string {
	return fmt.Sprintf("job_mcpt_%s_%d", strings.TrimPrefix(catalogExternalID, "mcpc_"), generation)
}

func nullableJSONArg(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func truncateDatabaseText(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
