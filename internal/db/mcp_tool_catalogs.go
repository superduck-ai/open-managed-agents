package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	mcpToolDiscoveryJobType              = "mcp_tool_discovery"
	mcpToolCatalogReferenceTouchInterval = 5 * time.Minute
)

type MCPToolCatalog struct {
	ID                  int64
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         int64
	TransportType       string
	EndpointURL         string
	EndpointKey         string
	AuthScopeKey        string
	AuthScopeReference  *string
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
	OrganizationID int64
	WorkspaceID    int64
	TransportType  string
	EndpointURL    string
	EndpointKey    string
	AuthScopeKey   string
	Trigger        string
	Force          bool
	Now            time.Time
}

type EnsureMCPToolCatalogResult struct {
	Catalog MCPToolCatalog
	Queued  bool
}

type MCPToolDiscoveryJob struct {
	ID                  int64
	ExternalID          string
	WorkspaceID         int64
	Payload             json.RawMessage
	Attempts            int
	CatalogExternalID   string
	CatalogOrganization int64
	EndpointURL         string
	EndpointKey         string
	Generation          int64
}

type CompleteMCPToolDiscoveryInput struct {
	JobID             int64
	WorkerID          string
	WorkspaceID       int64
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
	WorkspaceID       int64
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
	if input.AuthScopeKey == "" {
		input.AuthScopeKey = "anonymous"
	}
	externalID := mcpToolCatalogExternalID(input.OrganizationID, input.WorkspaceID, input.EndpointKey, input.AuthScopeKey)
	if existing, getErr := d.GetMCPToolCatalog(ctx, input.OrganizationID, input.WorkspaceID, input.EndpointKey, input.AuthScopeKey); getErr == nil {
		referenceIsRecent := !existing.LastReferencedAt.Before(input.Now.Add(-mcpToolCatalogReferenceTouchInterval))
		endpointIsCurrent := existing.TransportType == input.TransportType && existing.EndpointURL == input.EndpointURL
		if referenceIsRecent && endpointIsCurrent && canReuseMCPToolCatalog(existing, input) {
			return EnsureMCPToolCatalogResult{Catalog: existing}, nil
		}
	} else if !errors.Is(getErr, ErrNotFound) {
		return EnsureMCPToolCatalogResult{}, getErr
	}

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	catalog, err := scanMCPToolCatalog(tx.QueryRow(ctx, `
		insert into mcp_tool_catalogs (
			external_id, organization_id, workspace_id, transport_type,
			endpoint_url, endpoint_key, auth_scope_key, last_referenced_at,
			created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $8, $8)
		on conflict (organization_id, workspace_id, endpoint_key, auth_scope_key)
		do update set endpoint_url = excluded.endpoint_url,
			last_referenced_at = excluded.last_referenced_at,
			updated_at = excluded.updated_at
		returning `+mcpToolCatalogColumns()+`
	`, externalID, input.OrganizationID, input.WorkspaceID, input.TransportType,
		input.EndpointURL, input.EndpointKey, input.AuthScopeKey, input.Now))
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}

	if canReuseMCPToolCatalog(catalog, input) {
		if err := tx.Commit(ctx); err != nil {
			return EnsureMCPToolCatalogResult{}, err
		}
		return EnsureMCPToolCatalogResult{Catalog: catalog}, nil
	}

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
		"context_ref":         nil,
	})
	if err != nil {
		return EnsureMCPToolCatalogResult{}, err
	}
	jobExternalID := mcpToolDiscoveryJobExternalID(input.OrganizationID, input.WorkspaceID, catalog.ExternalID, generation)
	tag, err := tx.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload, run_after, created_at, updated_at)
		values ($1, $2, 'mcp_tool_discovery', 'pending', $3::jsonb, $4, $4, $4)
		on conflict (external_id) do nothing
	`, jobExternalID, input.WorkspaceID, jsonArg(payload), input.Now)
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

func (d *DB) GetMCPToolCatalog(ctx context.Context, organizationID, workspaceID int64, endpointKey, authScopeKey string) (MCPToolCatalog, error) {
	if authScopeKey == "" {
		authScopeKey = "anonymous"
	}
	return scanMCPToolCatalog(d.Pool.QueryRow(ctx, `
		select `+mcpToolCatalogColumns()+`
		from mcp_tool_catalogs
		where organization_id = $1
			and workspace_id = $2
			and endpoint_key = $3
			and auth_scope_key = $4
	`, organizationID, workspaceID, endpointKey, authScopeKey))
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
			returning j.id, j.external_id, j.workspace_id, j.payload, j.attempts
		)
		select l.id, l.external_id, l.workspace_id, l.payload, l.attempts,
			c.external_id, c.organization_id, c.endpoint_url, c.endpoint_key,
			coalesce((l.payload->>'generation')::bigint, 0)
		from leased l
		join mcp_tool_catalogs c
			on c.external_id = l.payload->>'catalog_external_id'
			and c.workspace_id = l.workspace_id
	`, limit, workerID, leaseDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []MCPToolDiscoveryJob
	for rows.Next() {
		var job MCPToolDiscoveryJob
		var payload []byte
		if err := rows.Scan(
			&job.ID, &job.ExternalID, &job.WorkspaceID, &payload, &job.Attempts,
			&job.CatalogExternalID, &job.CatalogOrganization, &job.EndpointURL, &job.EndpointKey,
			&job.Generation,
		); err != nil {
			return nil, err
		}
		job.Payload = copyRaw(payload)
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

	if err := settleMCPToolDiscoveryJob(ctx, tx, input.JobID, input.WorkerID, "completed", 0, time.Time{}, "", time.Time{}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		update mcp_tool_catalogs
		set tools = $4::jsonb,
			source = 'anonymous_probe',
			last_result_status = 'success',
			protocol_version = nullif($5, ''),
			server_info = $6::jsonb,
			catalog_hash = nullif($7, ''),
			discovered_at = $8,
			expires_at = $9,
			last_attempt_at = $8,
			last_error_code = null,
			last_error_message = null,
			last_error_at = null,
			retry_after = null,
			settled_generation = $3,
			updated_at = $8
		where external_id = $1
			and workspace_id = $2
			and requested_generation = $3
			and settled_generation < $3
	`, input.CatalogExternalID, input.WorkspaceID, input.Generation,
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
			set last_attempt_at = $4,
				updated_at = $4
			where external_id = $1
				and workspace_id = $2
				and requested_generation = $3
		`, input.CatalogExternalID, input.WorkspaceID, input.Generation, input.Now)
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
		set last_result_status = $4,
			last_attempt_at = $5,
			last_error_code = $6,
			last_error_message = nullif($7, ''),
			last_error_at = $5,
			retry_after = $8,
			settled_generation = $3,
			updated_at = $5
		where external_id = $1
			and workspace_id = $2
			and requested_generation = $3
			and settled_generation < $3
	`, input.CatalogExternalID, input.WorkspaceID, input.Generation,
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
	return `id, external_id, organization_id, workspace_id, transport_type,
		endpoint_url, endpoint_key, auth_scope_key, auth_scope_reference, tools,
		source, last_result_status, protocol_version, server_info, catalog_hash,
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
		&catalog.ID, &catalog.ExternalID, &catalog.OrganizationID, &catalog.WorkspaceID,
		&catalog.TransportType, &catalog.EndpointURL, &catalog.EndpointKey, &catalog.AuthScopeKey,
		&catalog.AuthScopeReference, &tools, &catalog.Source, &catalog.LastResultStatus,
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

func mcpToolCatalogExternalID(organizationID, workspaceID int64, endpointKey, authScopeKey string) string {
	hash := sha256.New()
	writeMCPHashInt64(hash, organizationID)
	writeMCPHashInt64(hash, workspaceID)
	hash.Write([]byte(endpointKey))
	hash.Write([]byte{0})
	hash.Write([]byte(authScopeKey))
	return "mcpc_" + hex.EncodeToString(hash.Sum(nil))[:40]
}

func mcpToolDiscoveryJobExternalID(organizationID, workspaceID int64, catalogExternalID string, generation int64) string {
	hash := sha256.New()
	hash.Write([]byte(mcpToolDiscoveryJobType))
	writeMCPHashInt64(hash, organizationID)
	writeMCPHashInt64(hash, workspaceID)
	hash.Write([]byte(catalogExternalID))
	writeMCPHashInt64(hash, generation)
	return "job_mcpt_" + hex.EncodeToString(hash.Sum(nil))[:40]
}

func writeMCPHashInt64(hash interface{ Write([]byte) (int, error) }, value int64) {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], uint64(value))
	_, _ = hash.Write(encoded[:])
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
