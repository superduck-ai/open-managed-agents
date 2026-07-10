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
	// JobWorkspaceID 只满足通用 jobs 表的任务归属要求，不参与全局 catalog identity。
	JobWorkspaceID int64
	TransportType  string
	EndpointURL    string
	Trigger        string
	Force          bool
	Now            time.Time
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
	if input.JobWorkspaceID <= 0 {
		return EnsureMCPToolCatalogResult{}, fmt.Errorf("job workspace id is required")
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
		"schema_version":      1,
		"catalog_external_id": catalog.ExternalID,
		"generation":          generation,
		"trigger":             input.Trigger,
	})
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	jobExternalID := mcpToolDiscoveryJobExternalID(catalog.ExternalID, generation)
	tag, err := tx.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload, run_after, created_at, updated_at)
		values ($1, $2, 'mcp_tool_discovery', 'pending', $3::jsonb, $4, $4, $4)
		on conflict (external_id) do nothing
	`, jobExternalID, input.JobWorkspaceID, jsonArg(payload), input.Now)
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
