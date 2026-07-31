package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type Agent struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	CurrentVersion      int
	Name                string
	Description         *string
	System              *string
	Model               json.RawMessage
	MCPServers          json.RawMessage
	Metadata            json.RawMessage
	Multiagent          json.RawMessage
	Skills              json.RawMessage
	Tools               json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
}

type agentRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	CurrentVersion      int        `db:"current_version"`
	Name                string     `db:"name"`
	Description         *string    `db:"description"`
	System              *string    `db:"system"`
	Model               []byte     `db:"model"`
	MCPServers          []byte     `db:"mcp_servers"`
	Metadata            []byte     `db:"metadata"`
	Multiagent          []byte     `db:"multiagent"`
	Skills              []byte     `db:"skills"`
	Tools               []byte     `db:"tools"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type AgentPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type AgentVersionPageCursor struct {
	Version int
	UUID    string
}

type ListAgentsPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type SearchAgentsPageParams struct {
	WorkspaceUUID   string
	Name            string
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
}

type ListAgentVersionsPageParams struct {
	WorkspaceUUID   string
	AgentExternalID string
	Limit           int
	Cursor          *AgentVersionPageCursor
}

type agentPageFilter struct {
	WorkspaceUUID   string
	Name            string
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

const createAgentSQL = `
	insert into agents (
		uuid, external_id, workspace_uuid, created_by_api_key_uuid, current_version,
		name, description, system, model, mcp_servers, metadata, multiagent,
		skills, tools, created_at, updated_at
	)
	values (
		CAST(:uuid AS uuid), :external_id, CAST(:workspace_uuid AS uuid),
		CAST(:created_by_api_key_uuid AS uuid), 1,
		:name, :description, :system, CAST(:model AS jsonb), CAST(:mcp_servers AS jsonb),
		CAST(:metadata AS jsonb), CAST(:multiagent AS jsonb), CAST(:skills AS jsonb),
		CAST(:tools AS jsonb), :created_at, :created_at
	)
	returning CAST(uuid AS text) AS uuid, external_id,
		CAST(workspace_uuid AS text) AS workspace_uuid,
		CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
		current_version, name, description, system, model,
		mcp_servers, metadata, multiagent, skills, tools, created_at, updated_at,
		archived_at, deleted_at
`

const updateAgentSQL = `
	update agents
	set current_version = :current_version,
		name = :name,
		description = :description,
		system = :system,
		model = CAST(:model AS jsonb),
		mcp_servers = CAST(:mcp_servers AS jsonb),
		metadata = CAST(:metadata AS jsonb),
		multiagent = CAST(:multiagent AS jsonb),
		skills = CAST(:skills AS jsonb),
		tools = CAST(:tools AS jsonb),
		updated_at = :updated_at
	where workspace_uuid = CAST(:workspace_uuid AS uuid)
		and external_id = :external_id
		and deleted_at is null
	returning CAST(uuid AS text) AS uuid, external_id,
		CAST(workspace_uuid AS text) AS workspace_uuid,
		CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
		current_version, name, description, system, model,
		mcp_servers, metadata, multiagent, skills, tools, created_at, updated_at,
		archived_at, deleted_at
`

const insertAgentVersionSQL = `
	insert into agent_versions (
		external_id, workspace_uuid, agent_uuid, agent_external_id, version,
		name, description, system, model, mcp_servers, metadata, multiagent,
		skills, tools, agent_created_at, agent_updated_at, archived_at
	)
	values (
		:version_external_id, CAST(:workspace_uuid AS uuid), CAST(:agent_uuid AS uuid),
		:agent_external_id, :version,
		:name, :description, :system, CAST(:model AS jsonb), CAST(:mcp_servers AS jsonb),
		CAST(:metadata AS jsonb), CAST(:multiagent AS jsonb), CAST(:skills AS jsonb),
		CAST(:tools AS jsonb), :agent_created_at, :agent_updated_at, :archived_at
	)
`

func (d *DB) CreateAgent(ctx context.Context, agent Agent, versionExternalID string) (Agent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Agent{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	created, err := getAgentSQLX(ctx, tx, createAgentSQL, agentArguments(agent))
	if err != nil {
		return Agent{}, err
	}
	if err := insertAgentVersion(ctx, tx, created, versionExternalID); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Agent{}, err
	}
	return created, nil
}

func (d *DB) GetAgent(ctx context.Context, workspaceUUID string, externalID string) (Agent, error) {
	return getAgentSQLX(ctx, d.sql, agentSelectSQL()+`
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and external_id = :external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
}

func (d *DB) GetAgentVersion(ctx context.Context, workspaceUUID string, externalID string, version int) (Agent, error) {
	if version < 1 {
		return Agent{}, ErrNotFound
	}
	return getAgentSQLX(ctx, d.sql, agentVersionSelectSQL()+`
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and agent_external_id = :external_id
			and version = :version
	`, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
		"version":        version,
	})
}

func (d *DB) UpdateAgent(ctx context.Context, workspaceUUID string, externalID string, expectedVersion int, next Agent, versionExternalID string) (Agent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Agent{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := getAgentSQLX(ctx, tx, agentSelectSQL()+`
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and external_id = :external_id
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
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
		return current, tx.Commit()
	}

	arguments := agentArguments(next)
	arguments["workspace_uuid"] = workspaceUUID
	arguments["external_id"] = externalID
	arguments["current_version"] = current.CurrentVersion + 1
	updated, err := getAgentSQLX(ctx, tx, updateAgentSQL, arguments)
	if err != nil {
		return Agent{}, err
	}
	if err := insertAgentVersion(ctx, tx, updated, versionExternalID); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(); err != nil {
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

func (d *DB) ArchiveAgent(ctx context.Context, workspaceUUID string, externalID string) (Agent, error) {
	return getAgentSQLX(ctx, d.sql, `
		update agents
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and external_id = :external_id
			and deleted_at is null
		returning CAST(uuid AS text) AS uuid, external_id,
			CAST(workspace_uuid AS text) AS workspace_uuid,
			CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
			current_version, name, description, system, model,
			mcp_servers, metadata, multiagent, skills, tools, created_at, updated_at,
			archived_at, deleted_at
	`, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
}

func (d *DB) ListAgentsPage(ctx context.Context, params ListAgentsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	query, arguments := agentPageQuery(agentPageFilter{
		WorkspaceUUID:   params.WorkspaceUUID,
		Limit:           limit,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
		CreatedAtGTE:    params.CreatedAtGTE,
		CreatedAtLTE:    params.CreatedAtLTE,
	})
	agents, err := selectAgentsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(agents, limit)
}

func (d *DB) SearchAgentsPage(ctx context.Context, params SearchAgentsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	query, arguments := agentPageQuery(agentPageFilter{
		WorkspaceUUID:   params.WorkspaceUUID,
		Name:            strings.TrimSpace(params.Name),
		Limit:           limit,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
	})
	agents, err := selectAgentsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(agents, limit)
}

func (d *DB) ListAgentVersionsPage(ctx context.Context, params ListAgentVersionsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	var agentUUID string
	err := namedGetContext(ctx, d.sql, &agentUUID, `
		select CAST(uuid AS text)
		from agents
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and external_id = :external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid": params.WorkspaceUUID,
		"external_id":    params.AgentExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}

	query, arguments := agentVersionsPageQuery(agentUUID, params.Cursor, limit)
	versions, err := selectAgentsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(versions, limit)
}

func insertAgentVersion(ctx context.Context, tx *sqlx.Tx, agent Agent, versionExternalID string) error {
	arguments := agentArguments(agent)
	arguments["version_external_id"] = versionExternalID
	arguments["agent_uuid"] = agent.UUID
	arguments["agent_external_id"] = agent.ExternalID
	arguments["version"] = agent.CurrentVersion
	arguments["agent_created_at"] = agent.CreatedAt
	arguments["agent_updated_at"] = agent.UpdatedAt
	arguments["archived_at"] = agent.ArchivedAt
	_, err := namedExecContext(ctx, tx, insertAgentVersionSQL, arguments)
	return err
}

func agentPageQuery(filter agentPageFilter) (string, map[string]any) {
	query := agentSelectSQL() + `
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": filter.WorkspaceUUID,
		"limit":          filter.Limit + 1,
	}
	if !filter.IncludeArchived {
		query += " and archived_at is null"
	}
	if filter.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *filter.CreatedAtGTE
	}
	if filter.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *filter.CreatedAtLTE
	}
	if filter.Name != "" {
		query += " and position(lower(:name) in lower(name)) > 0"
		arguments["name"] = filter.Name
	}
	if filter.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < CAST(:cursor_uuid AS uuid)))"
		arguments["cursor_created_at"] = filter.Cursor.CreatedAt
		arguments["cursor_uuid"] = filter.Cursor.UUID
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func agentVersionsPageQuery(agentUUID string, cursor *AgentVersionPageCursor, limit int) (string, map[string]any) {
	query := agentVersionSelectSQL() + `
		where agent_uuid = CAST(:agent_uuid AS uuid)
	`
	arguments := map[string]any{
		"agent_uuid": agentUUID,
		"limit":      limit + 1,
	}
	if cursor != nil {
		query += " and (version < :cursor_version or (version = :cursor_version and uuid < CAST(:cursor_uuid AS uuid)))"
		arguments["cursor_version"] = cursor.Version
		arguments["cursor_uuid"] = cursor.UUID
	}
	query += " order by version desc, uuid desc limit :limit"
	return query, arguments
}

func agentSelectSQL() string {
	return `
		select CAST(uuid AS text) AS uuid, external_id,
			CAST(workspace_uuid AS text) AS workspace_uuid,
			CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
			current_version, name, description, system, model,
			mcp_servers, metadata, multiagent, skills, tools, created_at, updated_at,
			archived_at, deleted_at
		from agents
	`
}

func agentVersionSelectSQL() string {
	return `
		select CAST(uuid AS text) AS uuid, agent_external_id AS external_id,
			CAST(workspace_uuid AS text) AS workspace_uuid,
			CAST('' AS text) AS created_by_api_key_uuid,
			version AS current_version, name, description, system, model, mcp_servers,
			metadata, multiagent, skills, tools, agent_created_at AS created_at,
			agent_updated_at AS updated_at, archived_at,
			CAST(null AS timestamptz) AS deleted_at
		from agent_versions
	`
}

func getAgentSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (Agent, error) {
	var row agentRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, err
	}
	return row.agent(), nil
}

func selectAgentsSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]Agent, error) {
	var rows []agentRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	agents := make([]Agent, len(rows))
	for index := range rows {
		agents[index] = rows[index].agent()
	}
	return agents, nil
}

func (row agentRow) agent() Agent {
	return Agent{
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		WorkspaceUUID:       row.WorkspaceUUID,
		CreatedByAPIKeyUUID: row.CreatedByAPIKeyUUID,
		CurrentVersion:      row.CurrentVersion,
		Name:                row.Name,
		Description:         row.Description,
		System:              row.System,
		Model:               copyRaw(row.Model),
		MCPServers:          copyRaw(row.MCPServers),
		Metadata:            copyRaw(row.Metadata),
		Multiagent:          copyRaw(row.Multiagent),
		Skills:              copyRaw(row.Skills),
		Tools:               copyRaw(row.Tools),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ArchivedAt:          row.ArchivedAt,
		DeletedAt:           row.DeletedAt,
	}
}

func agentArguments(agent Agent) map[string]any {
	return map[string]any{
		"uuid":                    agent.UUID,
		"external_id":             agent.ExternalID,
		"workspace_uuid":          agent.WorkspaceUUID,
		"created_by_api_key_uuid": agent.CreatedByAPIKeyUUID,
		"name":                    agent.Name,
		"description":             agent.Description,
		"system":                  agent.System,
		"model":                   jsonArg(agent.Model),
		"mcp_servers":             jsonArg(agent.MCPServers),
		"metadata":                jsonArg(agent.Metadata),
		"multiagent":              jsonArg(agent.Multiagent),
		"skills":                  jsonArg(agent.Skills),
		"tools":                   jsonArg(agent.Tools),
		"created_at":              agent.CreatedAt,
		"updated_at":              agent.UpdatedAt,
	}
}

func agentPageLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func trimAgentPage(agents []Agent, limit int) ([]Agent, bool, error) {
	hasMore := len(agents) > limit
	if hasMore {
		agents = agents[:limit]
	}
	return agents, hasMore, nil
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
