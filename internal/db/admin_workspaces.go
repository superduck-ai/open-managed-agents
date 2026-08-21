package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AdminWorkspace struct {
	UUID             uuid.UUID       `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID uuid.UUID       `db:"organization_uuid"`
	Name             string          `db:"name"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
	ArchivedAt       *time.Time      `db:"archived_at"`
	CompartmentID    string          `db:"compartment_id"`
	DisplayColor     string          `db:"display_color"`
	ExternalKeyID    *string         `db:"external_key_id"`
	Tags             json.RawMessage `db:"tags"`
}

type ListAdminWorkspacesParams struct {
	OrganizationUUID string
	IncludeArchived  bool
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) CreateAdminWorkspace(ctx context.Context, workspace AdminWorkspace) (AdminWorkspace, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	created, err := mapper.Insert(ctx, insertAdminWorkspaceParams{
		UUID:             workspace.UUID.String(),
		ExternalID:       workspace.ExternalID,
		OrganizationUUID: workspace.OrganizationUUID.String(),
		Name:             workspace.Name,
		CreatedAt:        workspace.CreatedAt,
		CompartmentID:    workspace.CompartmentID,
		DisplayColor:     workspace.DisplayColor,
		ExternalKeyID:    workspace.ExternalKeyID,
		Tags:             workspace.Tags,
	})
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	workspace, err := mapper.FindByIdentifier(
		ctx,
		organizationUUID,
		externalID,
		tryParseDBUUIDIdentifierString(externalID),
	)
	return workspace, mapNoRows(err)
}

func (d *DB) ListAdminWorkspacesPage(ctx context.Context, params ListAdminWorkspacesParams) ([]AdminWorkspace, bool, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	var anchor *pagePosition
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	if cursorID != "" {
		value, found, err := mapper.FindPageAnchorByExternalID(ctx, params.OrganizationUUID, cursorID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		anchor = &value
	}
	before := params.AfterID == "" && params.BeforeID != ""
	workspaces, err := mapper.ListPage(
		ctx,
		params.OrganizationUUID,
		params.IncludeArchived,
		anchor,
		before,
		params.Limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(workspaces, params.Limit), len(workspaces) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspace(ctx context.Context, organizationUUID, externalID string, next AdminWorkspace) (AdminWorkspace, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	updated, err := mapper.UpdateByExternalID(ctx, updateAdminWorkspaceParams{
		OrganizationUUID: organizationUUID,
		ExternalID:       externalID,
		Name:             next.Name,
		ExternalKeyID:    next.ExternalKeyID,
		Tags:             next.Tags,
		UpdatedAt:        next.UpdatedAt,
	})
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return updated, mapNoRows(err)
}

func (d *DB) ArchiveAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	workspace, err := mapper.ArchiveByExternalID(ctx, organizationUUID, externalID)
	return workspace, mapNoRows(err)
}

func (d *DB) CountAdminExternalKeyWorkspaceRefs(ctx context.Context, organizationUUID, externalID string) (int, error) {
	mapper := NewAdminWorkspaceMapper(d.mapperDB)
	return mapper.CountByExternalKeyID(ctx, organizationUUID, externalID)
}
