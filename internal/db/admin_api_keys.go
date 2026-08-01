package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminAPIKey struct {
	UUID                    uuid.UUID     `db:"uuid"`
	ExternalID              string        `db:"external_id"`
	WorkspaceUUID           uuid.UUID     `db:"workspace_uuid"`
	WorkspaceExternalID     string        `db:"workspace_external_id"`
	CreatedByUserUUID       uuid.NullUUID `db:"created_by_user_uuid"`
	CreatedByUserExternalID *string       `db:"created_by_user_external_id"`
	Name                    string        `db:"name"`
	PartialKeyHint          string        `db:"partial_key_hint"`
	Status                  string        `db:"status"`
	CreatedAt               time.Time     `db:"created_at"`
	UpdatedAt               time.Time     `db:"updated_at"`
	ExpiresAt               *time.Time    `db:"expires_at"`
}

type ListAdminAPIKeysParams struct {
	OrganizationUUID        string
	WorkspaceExternalID     string
	CreatedByUserExternalID string
	Status                  string
	AfterID                 string
	BeforeID                string
	Limit                   int
}

func (d *DB) GetAdminAPIKey(ctx context.Context, organizationUUID, externalID string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, adminAPIKeySelectSQL()+`
		where w.organization_uuid = :organization_uuid
			and ak.external_id = :external_id
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) ListAdminAPIKeysPage(ctx context.Context, params ListAdminAPIKeysParams) ([]AdminAPIKey, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"api_keys",
		"created_at",
		"external_id = :cursor_external_id",
		map[string]any{"cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminAPIKeySelectSQL() + ` where w.organization_uuid = :organization_uuid`
	args := map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "limit": params.Limit + 1}
	if params.WorkspaceExternalID != "" {
		query += " and w.external_id = :workspace_external_id"
		args["workspace_external_id"] = params.WorkspaceExternalID
	}
	if params.CreatedByUserExternalID != "" {
		query += " and u.external_id = :created_by_user_external_id"
		args["created_by_user_external_id"] = params.CreatedByUserExternalID
	}
	if params.Status != "" {
		query += " and ak.status = :status"
		args["status"] = params.Status
	}
	query = appendCursorFilter(query, args, "ak.created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by ak.created_at desc, ak.uuid desc limit :limit"
	keys, err := selectAdminRows[AdminAPIKey](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminAPIKey(ctx context.Context, organizationUUID, externalID string, setName bool, name string, setStatus bool, status string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, `
		with updated as (
			update api_keys ak
			set name = case when :set_name then :name else ak.name end,
				status = case when :set_status then :status else ak.status end,
				updated_at = now()
			from workspaces w
			where ak.workspace_uuid = w.uuid
				and w.organization_uuid = :organization_uuid
				and ak.external_id = :external_id
			returning ak.uuid, ak.external_id,
				ak.workspace_uuid,
				ak.created_by_user_uuid,
				ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		)
		select ak.uuid, ak.external_id, ak.workspace_uuid,
			w.external_id as workspace_external_id,
			ak.created_by_user_uuid,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from updated ak
		join workspaces w on w.uuid = ak.workspace_uuid
		left join users u on u.uuid = ak.created_by_user_uuid
	`, map[string]any{
		"organization_uuid": dbUUID(organizationUUID),
		"external_id":       externalID,
		"set_name":          setName,
		"name":              name,
		"set_status":        setStatus,
		"status":            status,
	})
}

func adminAPIKeySelectSQL() string {
	return `
		select ak.uuid, ak.external_id,
			ak.workspace_uuid,
			w.external_id as workspace_external_id,
			ak.created_by_user_uuid,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from api_keys ak
		join workspaces w on w.uuid = ak.workspace_uuid
		left join users u on u.uuid = ak.created_by_user_uuid
	`
}
