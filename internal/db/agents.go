package db

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
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

func (d *DB) CreateAgent(ctx context.Context, agent Agent, versionExternalID string) (Agent, error) {
	var created Agent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewAgentMapper(executor)
		row, insertErr := mapper.Insert(ctx, insertAgentParams{
			UUID:                agent.UUID,
			ExternalID:          agent.ExternalID,
			WorkspaceUUID:       agent.WorkspaceUUID,
			CreatedByAPIKeyUUID: nullableString(agent.CreatedByAPIKeyUUID),
			Config:              newAgentConfigParams(agent),
			CreatedAt:           agent.CreatedAt,
		})
		if insertErr != nil {
			return insertErr
		}
		created = row.agent()
		return mapper.InsertVersion(ctx, newInsertAgentVersionParams(created, versionExternalID))
	})
	return created, err
}

func (d *DB) GetAgent(ctx context.Context, workspaceUUID string, externalID string) (Agent, error) {
	mapper := NewAgentMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return agentFromRow(row, err)
}

func (d *DB) GetAgentVersion(ctx context.Context, workspaceUUID string, externalID string, version int) (Agent, error) {
	if version < 1 {
		return Agent{}, ErrNotFound
	}
	mapper := NewAgentMapper(d.mapperDB)
	row, err := mapper.FindVersion(ctx, workspaceUUID, externalID, version)
	return agentFromRow(row, err)
}

func (d *DB) UpdateAgent(ctx context.Context, workspaceUUID string, externalID string, expectedVersion int, next Agent, versionExternalID string) (Agent, error) {
	var updated Agent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewAgentMapper(executor)
		row, lockErr := mapper.LockByExternalID(ctx, workspaceUUID, externalID)
		if lockErr != nil {
			return mapNoRows(lockErr)
		}
		current := row.agent()
		if current.ArchivedAt != nil {
			return ErrInvalidState
		}
		if current.CurrentVersion != expectedVersion {
			return ErrVersionConflict
		}
		if sameAgentConfig(current, next) {
			updated = current
			return nil
		}

		row, updateErr := mapper.UpdateByExternalID(ctx, updateAgentParams{
			WorkspaceUUID:  workspaceUUID,
			ExternalID:     externalID,
			CurrentVersion: current.CurrentVersion + 1,
			Config:         newAgentConfigParams(next),
			UpdatedAt:      next.UpdatedAt,
		})
		if updateErr != nil {
			return mapNoRows(updateErr)
		}
		updated = row.agent()
		return mapper.InsertVersion(ctx, newInsertAgentVersionParams(updated, versionExternalID))
	})
	return updated, err
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
	var archived Agent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, err := NewAgentMapper(executor).ArchiveByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		archived = row.agent()
		schedules, err := NewDeploymentMapper(executor).ArchiveByRootAgent(ctx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		for _, schedule := range schedules {
			if len(schedule.Schedule) == 0 {
				continue
			}
			if err := d.applyDeploymentScheduleTxHook(ctx, executor, Deployment{
				WorkspaceUUID: schedule.WorkspaceUUID,
				ExternalID:    schedule.ExternalID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return archived, err
}

func (d *DB) ListAgentsPage(ctx context.Context, params ListAgentsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	mapper := NewAgentMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, agentPageFilter{
		WorkspaceUUID:   params.WorkspaceUUID,
		Limit:           limit + 1,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
		CreatedAtGTE:    params.CreatedAtGTE,
		CreatedAtLTE:    params.CreatedAtLTE,
	})
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(agentsFromRows(rows), limit)
}

func (d *DB) SearchAgentsPage(ctx context.Context, params SearchAgentsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	mapper := NewAgentMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, agentPageFilter{
		WorkspaceUUID:   params.WorkspaceUUID,
		Name:            strings.TrimSpace(params.Name),
		Limit:           limit + 1,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
	})
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(agentsFromRows(rows), limit)
}

func (d *DB) ListAgentVersionsPage(ctx context.Context, params ListAgentVersionsPageParams) ([]Agent, bool, error) {
	limit := agentPageLimit(params.Limit)
	mapper := NewAgentMapper(d.mapperDB)
	agentUUID, err := mapper.FindUUIDByExternalID(ctx, params.WorkspaceUUID, params.AgentExternalID)
	if err != nil {
		return nil, false, mapNoRows(err)
	}
	rows, err := mapper.ListVersionsPage(ctx, agentUUID, params.Cursor, limit+1)
	if err != nil {
		return nil, false, err
	}
	return trimAgentPage(agentsFromRows(rows), limit)
}

func (row agentRow) agent() Agent {
	createdByAPIKeyUUID := ""
	if row.CreatedByAPIKeyUUID != nil {
		createdByAPIKeyUUID = *row.CreatedByAPIKeyUUID
	}
	return Agent{
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		WorkspaceUUID:       row.WorkspaceUUID,
		CreatedByAPIKeyUUID: createdByAPIKeyUUID,
		CurrentVersion:      row.CurrentVersion,
		Name:                row.Name,
		Description:         row.Description,
		System:              row.System,
		Model:               bytes.Clone(row.Model),
		MCPServers:          bytes.Clone(row.MCPServers),
		Metadata:            bytes.Clone(row.Metadata),
		Multiagent:          bytes.Clone(row.Multiagent),
		Skills:              bytes.Clone(row.Skills),
		Tools:               bytes.Clone(row.Tools),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ArchivedAt:          row.ArchivedAt,
		DeletedAt:           row.DeletedAt,
	}
}

func agentFromRow(row agentRow, err error) (Agent, error) {
	if err != nil {
		return Agent{}, mapNoRows(err)
	}
	return row.agent(), nil
}

func agentsFromRows(rows []agentRow) []Agent {
	agents := make([]Agent, len(rows))
	for index := range rows {
		agents[index] = rows[index].agent()
	}
	return agents
}

func newAgentConfigParams(agent Agent) agentConfigParams {
	return agentConfigParams{
		Name:        agent.Name,
		Description: agent.Description,
		System:      agent.System,
		Model:       agentJSONArg(agent.Model),
		MCPServers:  agentJSONArg(agent.MCPServers),
		Metadata:    agentJSONArg(agent.Metadata),
		Multiagent:  agentJSONArg(agent.Multiagent),
		Skills:      agentJSONArg(agent.Skills),
		Tools:       agentJSONArg(agent.Tools),
	}
}

func newInsertAgentVersionParams(agent Agent, versionExternalID string) insertAgentVersionParams {
	return insertAgentVersionParams{
		ExternalID:      versionExternalID,
		WorkspaceUUID:   agent.WorkspaceUUID,
		AgentUUID:       agent.UUID,
		AgentExternalID: agent.ExternalID,
		Version:         agent.CurrentVersion,
		Config:          newAgentConfigParams(agent),
		AgentCreatedAt:  agent.CreatedAt,
		AgentUpdatedAt:  agent.UpdatedAt,
		ArchivedAt:      agent.ArchivedAt,
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

func agentJSONArg(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
