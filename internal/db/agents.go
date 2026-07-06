package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Agent struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	CreatedByAPIKeyID int64
	CurrentVersion    int
	Name              string
	Description       *string
	System            *string
	Model             json.RawMessage
	MCPServers        json.RawMessage
	Metadata          json.RawMessage
	Multiagent        json.RawMessage
	Skills            json.RawMessage
	Tools             json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
}

type AgentPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type AgentVersionPageCursor struct {
	Version int
	ID      int64
}

type ListAgentsPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type SearchAgentsPageParams struct {
	WorkspaceID     int64
	Name            string
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
}

type ListAgentVersionsPageParams struct {
	WorkspaceID     int64
	AgentExternalID string
	Limit           int
	Cursor          *AgentVersionPageCursor
}

func (d *DB) CreateAgent(ctx context.Context, agent Agent, versionExternalID string) (Agent, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback(ctx)

	created, err := scanAgent(tx.QueryRow(ctx, `
		insert into agents (
			uuid, external_id, workspace_id, created_by_api_key_id, current_version,
			name, description, system, model, mcp_servers, metadata, multiagent,
			skills, tools, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, 1,
			$5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb,
			$12::jsonb, $13::jsonb, $14, $14
		)
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			current_version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, created_at, updated_at, archived_at, deleted_at
	`, agent.UUID, agent.ExternalID, agent.WorkspaceID, agent.CreatedByAPIKeyID,
		agent.Name, agent.Description, agent.System, jsonArg(agent.Model), jsonArg(agent.MCPServers),
		jsonArg(agent.Metadata), jsonArg(agent.Multiagent), jsonArg(agent.Skills), jsonArg(agent.Tools),
		agent.CreatedAt))
	if err != nil {
		return Agent{}, err
	}
	if err := insertAgentVersion(ctx, tx, created, versionExternalID); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return created, nil
}

func (d *DB) GetAgent(ctx context.Context, workspaceID int64, externalID string) (Agent, error) {
	return scanAgent(d.Pool.QueryRow(ctx, agentSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) GetAgentVersion(ctx context.Context, workspaceID int64, externalID string, version int) (Agent, error) {
	if version < 1 {
		return Agent{}, ErrNotFound
	}
	return scanAgent(d.Pool.QueryRow(ctx, `
		select id, uuid::text, agent_external_id, workspace_id, 0::bigint,
			version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, agent_created_at, agent_updated_at, archived_at, null::timestamptz
		from agent_versions
		where workspace_id = $1 and agent_external_id = $2 and version = $3
	`, workspaceID, externalID, version))
}

func (d *DB) UpdateAgent(ctx context.Context, workspaceID int64, externalID string, expectedVersion int, next Agent, versionExternalID string) (Agent, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanAgent(tx.QueryRow(ctx, agentSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID))
	if err != nil {
		return Agent{}, err
	}
	if current.ArchivedAt != nil {
		return Agent{}, ErrInvalidState
	}
	if current.CurrentVersion != expectedVersion {
		return Agent{}, ErrVersionConflict
	}
	if sameAgentConfig(current, next) {
		return current, tx.Commit(ctx)
	}

	updated, err := scanAgent(tx.QueryRow(ctx, `
		update agents
		set current_version = $3,
			name = $4,
			description = $5,
			system = $6,
			model = $7::jsonb,
			mcp_servers = $8::jsonb,
			metadata = $9::jsonb,
			multiagent = $10::jsonb,
			skills = $11::jsonb,
			tools = $12::jsonb,
			updated_at = $13
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			current_version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, created_at, updated_at, archived_at, deleted_at
	`, workspaceID, externalID, current.CurrentVersion+1, next.Name, next.Description, next.System,
		jsonArg(next.Model), jsonArg(next.MCPServers), jsonArg(next.Metadata), jsonArg(next.Multiagent),
		jsonArg(next.Skills), jsonArg(next.Tools), next.UpdatedAt))
	if err != nil {
		return Agent{}, err
	}
	if err := insertAgentVersion(ctx, tx, updated, versionExternalID); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return updated, nil
}

func sameAgentConfig(left Agent, right Agent) bool {
	return left.Name == right.Name &&
		sameOptionalString(left.Description, right.Description) &&
		sameOptionalString(left.System, right.System) &&
		sameJSON(left.Model, right.Model) &&
		sameJSON(left.MCPServers, right.MCPServers) &&
		sameJSON(left.Metadata, right.Metadata) &&
		sameJSON(left.Multiagent, right.Multiagent) &&
		sameJSON(left.Skills, right.Skills) &&
		sameJSON(left.Tools, right.Tools)
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameJSON(left json.RawMessage, right json.RawMessage) bool {
	if isNullJSON(left) && isNullJSON(right) {
		return true
	}
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return string(left) == string(right)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return string(left) == string(right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func isNullJSON(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return value == nil
}

func (d *DB) ArchiveAgent(ctx context.Context, workspaceID int64, externalID string) (Agent, error) {
	return scanAgent(d.Pool.QueryRow(ctx, `
		update agents
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			current_version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, created_at, updated_at, archived_at, deleted_at
	`, workspaceID, externalID))
}

func (d *DB) ListAgentsPage(ctx context.Context, params ListAgentsPageParams) ([]Agent, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := agentSelectSQL() + `
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	agents, err := scanAgentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(agents) > params.Limit
	if hasMore {
		agents = agents[:params.Limit]
	}
	return agents, hasMore, nil
}

func (d *DB) SearchAgentsPage(ctx context.Context, params SearchAgentsPageParams) ([]Agent, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := agentSelectSQL() + `
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if strings.TrimSpace(params.Name) != "" {
		query += fmt.Sprintf(" and position(lower($%d) in lower(name)) > 0", nextArg)
		args = append(args, strings.TrimSpace(params.Name))
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	agents, err := scanAgentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(agents) > params.Limit
	if hasMore {
		agents = agents[:params.Limit]
	}
	return agents, hasMore, nil
}

func (d *DB) ListAgentVersionsPage(ctx context.Context, params ListAgentVersionsPageParams) ([]Agent, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	var agentID int64
	if err := d.Pool.QueryRow(ctx, `
		select id
		from agents
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, params.WorkspaceID, params.AgentExternalID).Scan(&agentID); errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}

	query := `
		select id, uuid::text, agent_external_id, workspace_id, 0::bigint,
			version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, agent_created_at, agent_updated_at, archived_at, null::timestamptz
		from agent_versions
		where agent_id = $1
	`
	args := []any{agentID}
	nextArg := 2
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (version < $%d or (version = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.Version, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by version desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	versions, err := scanAgentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

type agentTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertAgentVersion(ctx context.Context, tx agentTx, agent Agent, versionExternalID string) error {
	_, err := tx.Exec(ctx, `
		insert into agent_versions (
			external_id, workspace_id, agent_id, agent_external_id, version,
			name, description, system, model, mcp_servers, metadata, multiagent,
			skills, tools, agent_created_at, agent_updated_at, archived_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb,
			$13::jsonb, $14::jsonb, $15, $16, $17
		)
	`, versionExternalID, agent.WorkspaceID, agent.ID, agent.ExternalID, agent.CurrentVersion,
		agent.Name, agent.Description, agent.System, jsonArg(agent.Model), jsonArg(agent.MCPServers),
		jsonArg(agent.Metadata), jsonArg(agent.Multiagent), jsonArg(agent.Skills), jsonArg(agent.Tools),
		agent.CreatedAt, agent.UpdatedAt, agent.ArchivedAt)
	return err
}

func agentSelectSQL() string {
	return `
		select id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			current_version, name, description, system, model, mcp_servers, metadata,
			multiagent, skills, tools, created_at, updated_at, archived_at, deleted_at
		from agents
	`
}

type agentScanner interface {
	Scan(dest ...any) error
}

type agentRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanAgent(row agentScanner) (Agent, error) {
	var agent Agent
	var model, mcpServers, metadata, multiagent, skills, tools []byte
	err := row.Scan(&agent.ID, &agent.UUID, &agent.ExternalID, &agent.WorkspaceID, &agent.CreatedByAPIKeyID,
		&agent.CurrentVersion, &agent.Name, &agent.Description, &agent.System, &model, &mcpServers, &metadata,
		&multiagent, &skills, &tools, &agent.CreatedAt, &agent.UpdatedAt, &agent.ArchivedAt, &agent.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, err
	}
	agent.Model = copyRaw(model)
	agent.MCPServers = copyRaw(mcpServers)
	agent.Metadata = copyRaw(metadata)
	agent.Multiagent = copyRaw(multiagent)
	agent.Skills = copyRaw(skills)
	agent.Tools = copyRaw(tools)
	return agent, nil
}

func scanAgentRows(rows agentRows) ([]Agent, error) {
	var agents []Agent
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func copyRaw(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func jsonArg(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}
