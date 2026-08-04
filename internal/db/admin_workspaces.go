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

const countAdminExternalKeyWorkspaceRefsQuery = `
	select count(*)
	from workspaces
	where organization_uuid = :organization_uuid
		and external_key_id = :external_id
`

func (d *DB) CreateAdminWorkspace(ctx context.Context, workspace AdminWorkspace) (AdminWorkspace, error) {
	created, err := getAdminRow[AdminWorkspace](ctx, d.sql, `
		insert into workspaces (
			uuid, external_id, organization_uuid, name, created_at, updated_at,
			compartment_id, display_color, external_key_id, tags
		)
		values (
			:uuid, :external_id, :organization_uuid, :name, :created_at, :created_at,
			:compartment_id, :display_color, :external_key_id, CAST(:tags AS jsonb)
		)
		returning uuid, external_id,
			organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, adminWorkspaceArguments(workspace))
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, adminWorkspaceSelectSQL()+`
		where w.organization_uuid = :organization_uuid
			and (w.external_id = :external_id or w.uuid = :workspace_uuid)
	`, map[string]any{
		"organization_uuid": dbUUID(organizationUUID),
		"external_id":       externalID,
		"workspace_uuid":    tryParseDBUUIDIdentifier(externalID),
	})
}

func (d *DB) ListAdminWorkspacesPage(ctx context.Context, params ListAdminWorkspacesParams) ([]AdminWorkspace, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"workspaces",
		"created_at",
		"organization_uuid = :organization_uuid and external_id = :cursor_external_id",
		map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceSelectSQL() + ` where w.organization_uuid = :organization_uuid`
	args := map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "limit": params.Limit + 1}
	if !params.IncludeArchived {
		query += " and w.archived_at is null"
	}
	query = appendCursorFilter(query, args, "w.created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by w.created_at desc, w.uuid desc limit :limit"
	workspaces, err := selectAdminRows[AdminWorkspace](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(workspaces, params.Limit), len(workspaces) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspace(ctx context.Context, organizationUUID, externalID string, next AdminWorkspace) (AdminWorkspace, error) {
	args := adminWorkspaceArguments(next)
	args["organization_uuid"] = dbUUID(organizationUUID)
	args["external_id"] = externalID
	updated, err := getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set name = :name,
			external_key_id = :external_key_id,
			tags = CAST(:tags AS jsonb),
			updated_at = :updated_at
		where organization_uuid = :organization_uuid
			and external_id = :external_id
		returning uuid, external_id,
			organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, args)
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and external_id = :external_id
		returning uuid, external_id,
			organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) CountAdminExternalKeyWorkspaceRefs(ctx context.Context, organizationUUID, externalID string) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, countAdminExternalKeyWorkspaceRefsQuery, map[string]any{
		"organization_uuid": dbUUID(organizationUUID),
		"external_id":       externalID,
	})
	return count, err
}

func adminWorkspaceSelectSQL() string {
	return `
		select w.uuid, w.external_id,
			w.organization_uuid, w.name,
			w.created_at, w.updated_at, w.archived_at, w.compartment_id,
			w.display_color, w.external_key_id, w.tags
		from workspaces w
	`
}

func adminWorkspaceArguments(workspace AdminWorkspace) map[string]any {
	return map[string]any{
		"uuid":              workspace.UUID,
		"external_id":       workspace.ExternalID,
		"organization_uuid": workspace.OrganizationUUID,
		"name":              workspace.Name,
		"created_at":        workspace.CreatedAt,
		"updated_at":        workspace.UpdatedAt,
		"compartment_id":    workspace.CompartmentID,
		"display_color":     workspace.DisplayColor,
		"external_key_id":   workspace.ExternalKeyID,
		"tags":              jsonArg(workspace.Tags),
	}
}
