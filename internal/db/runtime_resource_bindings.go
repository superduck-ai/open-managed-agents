package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type RuntimeResourceBinding struct {
	ID                  int64           `db:"id"`
	OrganizationUUID    string          `db:"organization_uuid"`
	WorkspaceUUID       string          `db:"workspace_uuid"`
	RuntimeProfileUUID  string          `db:"runtime_profile_uuid"`
	ProviderKind        string          `db:"provider_kind"`
	ResourceType        string          `db:"resource_type"`
	OMAExternalID       string          `db:"oma_external_id"`
	RemoteID            string          `db:"remote_id"`
	ParentOMAExternalID string          `db:"parent_oma_external_id"`
	ResourceRole        string          `db:"resource_role"`
	State               string          `db:"state"`
	Summary             json.RawMessage `db:"summary"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

type ListRuntimeResourceBindingsParams struct {
	OrganizationUUID    string
	WorkspaceUUID       string
	ResourceType        string
	ParentOMAExternalID string
	ResourceRole        string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

type SearchRuntimeResourceBindingsParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ResourceType     string
	Query            string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

func (d *DB) UpsertRuntimeResourceBinding(ctx context.Context, binding RuntimeResourceBinding) (RuntimeResourceBinding, error) {
	row, err := NewRuntimeResourceBindingMapper(d.mapperDB).Upsert(ctx, runtimeResourceBindingUpsertParams(binding))
	if err != nil {
		if isUniqueViolation(err) {
			return RuntimeResourceBinding{}, ErrDuplicate
		}
		return RuntimeResourceBinding{}, err
	}
	return row, nil
}

func (d *DB) InsertRuntimeResourceBindingIfAbsent(ctx context.Context, binding RuntimeResourceBinding) (RuntimeResourceBinding, bool, error) {
	row, err := NewRuntimeResourceBindingMapper(d.mapperDB).InsertIfAbsent(ctx, runtimeResourceBindingUpsertParams(binding))
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeResourceBinding{}, false, nil
	}
	if err != nil {
		return RuntimeResourceBinding{}, false, err
	}
	return row, true, nil
}

func (d *DB) GetRuntimeResourceBindingByOMAID(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	resourceType string,
	omaExternalID string,
) (RuntimeResourceBinding, error) {
	row, err := NewRuntimeResourceBindingMapper(d.mapperDB).FindByOMAID(ctx, organizationUUID, workspaceUUID, resourceType, omaExternalID)
	return row, mapNoRows(err)
}

func (d *DB) GetRuntimeResourceBindingByRemoteID(
	ctx context.Context,
	runtimeProfileUUID string,
	resourceType string,
	remoteID string,
) (RuntimeResourceBinding, error) {
	row, err := NewRuntimeResourceBindingMapper(d.mapperDB).FindByRemoteID(ctx, runtimeProfileUUID, resourceType, remoteID)
	return row, mapNoRows(err)
}

func (d *DB) ListRuntimeResourceBindingsByWorkspaceAndType(
	ctx context.Context,
	params ListRuntimeResourceBindingsParams,
) ([]RuntimeResourceBinding, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	return NewRuntimeResourceBindingMapper(d.mapperDB).ListByWorkspaceAndType(ctx, listRuntimeResourceBindingsQuery{
		OrganizationUUID:    params.OrganizationUUID,
		WorkspaceUUID:       params.WorkspaceUUID,
		ResourceType:        params.ResourceType,
		ParentOMAExternalID: params.ParentOMAExternalID,
		ResourceRole:        params.ResourceRole,
		IncludeArchived:     params.IncludeArchived,
		Limit:               limit,
		Offset:              offset,
	})
}

func (d *DB) SearchRuntimeResourceBindingsByWorkspaceAndType(
	ctx context.Context,
	params SearchRuntimeResourceBindingsParams,
) ([]RuntimeResourceBinding, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + escapeILIKEPattern(params.Query) + "%"
	return NewRuntimeResourceBindingMapper(d.mapperDB).SearchByWorkspaceAndType(ctx, searchRuntimeResourceBindingsQuery{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		ResourceType:     params.ResourceType,
		Pattern:          pattern,
		IncludeArchived:  params.IncludeArchived,
		Limit:            limit,
		Offset:           offset,
	})
}

func (d *DB) UpdateRuntimeResourceBindingSummaryAndState(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	resourceType string,
	omaExternalID string,
	state string,
	summary json.RawMessage,
) (RuntimeResourceBinding, error) {
	if len(summary) == 0 {
		summary = json.RawMessage(`{}`)
	}
	row, err := NewRuntimeResourceBindingMapper(d.mapperDB).UpdateSummaryAndState(ctx, updateRuntimeResourceBindingParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ResourceType:     resourceType,
		OMAExternalID:    omaExternalID,
		State:            state,
		Summary:          string(summary),
	})
	return row, mapNoRows(err)
}

func (d *DB) SoftDeleteRuntimeResourceBinding(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	resourceType string,
	omaExternalID string,
) error {
	rowsAffected, err := NewRuntimeResourceBindingMapper(d.mapperDB).SoftDelete(ctx, organizationUUID, workspaceUUID, resourceType, omaExternalID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) LockAgentRuntimeResourceBindingInTx(
	ctx context.Context,
	executor yourbatis.Executor,
	organizationUUID string,
	workspaceUUID string,
	omaExternalID string,
) (RuntimeResourceBinding, error) {
	row, err := NewRuntimeResourceBindingMapper(executor).LockAgentInTx(ctx, organizationUUID, workspaceUUID, omaExternalID)
	return row, mapNoRows(err)
}

func (d *DB) AggregateExternalWorkspaceOverview(
	ctx context.Context,
	query WorkspaceOverviewQuery,
) ([]WorkspaceOverviewBucket, error) {
	return NewRuntimeResourceBindingMapper(d.mapperDB).AggregateExternalOverview(ctx, query)
}

func runtimeResourceBindingUpsertParams(binding RuntimeResourceBinding) upsertRuntimeResourceBindingParams {
	state := binding.State
	if state == "" {
		state = "active"
	}
	summary := binding.Summary
	if len(summary) == 0 {
		summary = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	createdAt := binding.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := binding.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	return upsertRuntimeResourceBindingParams{
		OrganizationUUID:    binding.OrganizationUUID,
		WorkspaceUUID:       binding.WorkspaceUUID,
		RuntimeProfileUUID:  binding.RuntimeProfileUUID,
		ProviderKind:        binding.ProviderKind,
		ResourceType:        binding.ResourceType,
		OMAExternalID:       binding.OMAExternalID,
		RemoteID:            binding.RemoteID,
		ParentOMAExternalID: binding.ParentOMAExternalID,
		ResourceRole:        binding.ResourceRole,
		State:               state,
		Summary:             string(summary),
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}
}

func escapeILIKEPattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
