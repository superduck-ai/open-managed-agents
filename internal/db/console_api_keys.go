package db

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type consoleAPIKeyScanner interface {
	Scan(dest ...any) error
}

func scanConsoleAPIKey(row consoleAPIKeyScanner) (platform.ConsoleAPIKey, error) {
	var key platform.ConsoleAPIKey
	err := row.Scan(
		&key.ID,
		&key.OrgUUID,
		&key.WorkspaceID,
		&key.Name,
		&key.KeyPrefix,
		&key.KeySuffix,
		&key.Status,
		&key.CreatedByUserUUID,
		&key.LastUsedAt,
		&key.ExpiresAt,
		&key.ArchivedAt,
		&key.CreatedAt,
		&key.UpdatedAt,
	)
	if err != nil {
		return platform.ConsoleAPIKey{}, mapNoRows(err)
	}
	return key, nil
}

func (d *DB) ListConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID *string) ([]platform.ConsoleAPIKey, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleAPIKey{}, nil
	}
	query := `
		select
			external_id,
			org_uuid,
			workspace_id,
			name,
			key_prefix,
			key_suffix,
			status,
			created_by_user_uuid,
			last_used_at,
			expires_at,
			archived_at,
			created_at,
			updated_at
		from console_api_keys
		where org_uuid = $1
	`
	args := []any{strings.TrimSpace(orgUUID)}
	if workspaceID != nil {
		query += ` and workspace_id = $2`
		args = append(args, strings.TrimSpace(*workspaceID))
	}
	query += ` order by created_at desc, id desc`

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		if isUndefinedRelationError(err) {
			return []platform.ConsoleAPIKey{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var out []platform.ConsoleAPIKey
	for rows.Next() {
		key, err := scanConsoleAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (d *DB) CreateConsoleAPIKey(ctx context.Context, input platform.CreateConsoleAPIKeyInput) (platform.CreateConsoleAPIKeyResult, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(input.OrgUUID) == "" || strings.TrimSpace(input.WorkspaceID) == "" {
		return platform.CreateConsoleAPIKeyResult{}, platform.ErrNotFound
	}
	orgUUID := strings.TrimSpace(input.OrgUUID)
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	rawKey := "sk-ant-api03-" + consoleRandomToken(32)
	keyPrefix := rawKey
	if len(keyPrefix) > 16 {
		keyPrefix = keyPrefix[:16]
	}
	keySuffix := rawKey
	if len(keySuffix) > 6 {
		keySuffix = keySuffix[len(keySuffix)-6:]
	}
	externalID := consolePrefixedID("apikey", 18)
	keyHash := auth.HashAPIKey(rawKey)

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	coreWorkspaceID, coreUserID, err := d.resolveConsoleAPIKeyCoreRefs(ctx, tx, orgUUID, workspaceID, input.CreatedByUserUUID)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}

	key, err := scanConsoleAPIKey(tx.QueryRow(ctx, `
		insert into console_api_keys (
			external_id,
			api_key_uuid,
			org_uuid,
			workspace_id,
			name,
			key_prefix,
			key_suffix,
			key_hash,
			status,
			created_by_user_uuid,
			expires_at
		)
		values ($1, $1, $2, $3, $4, $5, $6, $7, 'active', $8, $9)
		returning
			external_id,
			org_uuid,
			workspace_id,
			name,
			key_prefix,
			key_suffix,
			status,
			created_by_user_uuid,
			last_used_at,
			expires_at,
			archived_at,
			created_at,
			updated_at
	`,
		externalID,
		orgUUID,
		workspaceID,
		strings.TrimSpace(input.Name),
		keyPrefix,
		keySuffix,
		keyHash,
		input.CreatedByUserUUID,
		input.ExpiresAt,
	))
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into api_keys (
			external_id,
			workspace_id,
			key_hash,
			status,
			created_by_user_id,
			name,
			partial_key_hint,
			expires_at
		)
		values ($1, $2, $3, 'active', $4, $5, $6, $7)
	`, externalID, coreWorkspaceID, keyHash, coreUserID, strings.TrimSpace(input.Name), partialAPIKeyHint(rawKey), input.ExpiresAt); err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	return platform.CreateConsoleAPIKeyResult{
		APIKey: key,
		RawKey: rawKey,
	}, nil
}

func (d *DB) UpdateConsoleAPIKeyStatus(ctx context.Context, input platform.UpdateConsoleAPIKeyStatusInput) (platform.ConsoleAPIKey, error) {
	if d == nil || d.Pool == nil ||
		strings.TrimSpace(input.OrgUUID) == "" ||
		strings.TrimSpace(input.WorkspaceID) == "" ||
		strings.TrimSpace(input.APIKeyID) == "" ||
		strings.TrimSpace(input.Status) == "" {
		return platform.ConsoleAPIKey{}, platform.ErrNotFound
	}
	orgUUID := strings.TrimSpace(input.OrgUUID)
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	apiKeyID := strings.TrimSpace(input.APIKeyID)
	status := strings.TrimSpace(input.Status)

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	key, err := scanConsoleAPIKey(tx.QueryRow(ctx, `
		update console_api_keys
		set status = $4,
			archived_at = case
				when $4 = 'archived' then coalesce(archived_at, now())
				else null
			end,
			updated_at = now()
		where org_uuid = $1
		  and workspace_id = $2
		  and (external_id = $3 or api_key_uuid = $3)
		returning
			external_id,
			org_uuid,
			workspace_id,
			name,
			key_prefix,
			key_suffix,
			status,
			created_by_user_uuid,
			last_used_at,
			expires_at,
			archived_at,
			created_at,
			updated_at
	`, orgUUID, workspaceID, apiKeyID, status))
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}

	if _, err := tx.Exec(ctx, `
		update api_keys
		set status = $2,
			updated_at = now()
		where external_id = $1
	`, key.ID, status); err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	return key, nil
}

func (d *DB) resolveConsoleAPIKeyCoreRefs(ctx context.Context, tx pgx.Tx, orgUUID string, workspaceID string, userUUID *string) (int64, *int64, error) {
	var coreWorkspaceID int64
	if workspaceID == "default" {
		err := tx.QueryRow(ctx, `
			select w.id
			from organizations o
			join lateral (
				select id
				from workspaces
				where organization_id = o.id
				  and archived_at is null
				order by
					case
						when external_id = 'workspace_default' then 0
						when lower(name) = 'default' then 1
						else 2
					end,
					created_at asc,
					id asc
				limit 1
			) w on true
			where o.uuid::text = $1 or o.external_id = $1
			limit 1
		`, orgUUID).Scan(&coreWorkspaceID)
		if err != nil {
			return 0, nil, mapNoRows(err)
		}
	} else {
		err := tx.QueryRow(ctx, `
			select w.id
			from workspaces w
			join organizations o on o.id = w.organization_id
			where (o.uuid::text = $1 or o.external_id = $1)
			  and (w.external_id = $2 or w.uuid::text = $2)
			  and w.archived_at is null
			limit 1
		`, orgUUID, workspaceID).Scan(&coreWorkspaceID)
		if err != nil {
			return 0, nil, mapNoRows(err)
		}
	}

	var coreUserID *int64
	if userUUID != nil && strings.TrimSpace(*userUUID) != "" {
		var userID int64
		err := tx.QueryRow(ctx, `
			select u.id
			from users u
			join organizations o on o.id = u.organization_id
			where (o.uuid::text = $1 or o.external_id = $1)
			  and u.deleted_at is null
			  and (
				u.external_id = $2
				or u.uuid::text = $2
				or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
			  )
			limit 1
		`, orgUUID, strings.TrimSpace(*userUUID)).Scan(&userID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, err
		}
		if err == nil {
			coreUserID = &userID
		}
	}
	return coreWorkspaceID, coreUserID, nil
}

func (d *DB) CountConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID string) (int, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceID) == "" {
		return 0, nil
	}
	var count int
	err := d.Pool.QueryRow(ctx, `
		select count(*)
		from console_api_keys
		where org_uuid = $1
		  and workspace_id = $2
		  and archived_at is null
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(workspaceID)).Scan(&count)
	if isUndefinedRelationError(err) {
		return 0, nil
	}
	return count, err
}

func (d *DB) CreateConsoleWorkspace(ctx context.Context, input platform.CreateConsoleWorkspaceInput) (platform.ConsoleWorkspace, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(input.OrgUUID) == "" || strings.TrimSpace(input.Name) == "" {
		return platform.ConsoleWorkspace{}, platform.ErrNotFound
	}
	externalID := consolePrefixedID("wrkspc", 18)
	dataResidency, err := consoleWorkspaceDataResidencyJSON(input.DataResidency)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	workspace, err := scanConsoleWorkspace(d.Pool.QueryRow(ctx, `
		with org as (
			select id, uuid::text as org_uuid
			from organizations
			where uuid::text = $1 or external_id = $1
			limit 1
		)
		insert into workspaces (
			uuid,
			external_id,
			organization_id,
			name,
			compartment_id,
			display_color,
			data_residency,
			tags
		)
		select $2, $3, org.id, $4, $3, $5, $6::jsonb, '{}'::jsonb
		from org
		on conflict (organization_id, name) do update set
			display_color = excluded.display_color,
			data_residency = excluded.data_residency,
			archived_at = null,
			updated_at = now()
		returning
			external_id,
			(select org_uuid from org),
			name,
			display_color,
			display_color,
			data_residency,
			external_key_id,
			tags,
			archived_at,
			created_at,
			updated_at
	`, strings.TrimSpace(input.OrgUUID), uuid.NewString(), externalID, strings.TrimSpace(input.Name), firstNonEmpty(strings.TrimSpace(input.DisplayColor), strings.TrimSpace(input.Color), "#9B87F5"), dataResidency))
	if isUniqueViolation(err) {
		return platform.ConsoleWorkspace{}, err
	}
	return workspace, err
}

func (d *DB) ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]platform.ConsoleWorkspace, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleWorkspace{}, nil
	}
	archivedFilter := "and w.archived_at is null"
	if includeArchived {
		archivedFilter = ""
	}
	rows, err := d.Pool.Query(ctx, `
		select
			w.external_id,
			o.uuid::text,
			w.name,
			w.display_color,
			w.display_color,
			w.data_residency,
			w.external_key_id,
			w.tags,
			w.archived_at,
			w.created_at,
			w.updated_at
		from workspaces w
		join organizations o on o.id = w.organization_id
		where (o.external_id = $1 or o.uuid::text = $1)
		`+archivedFilter+`
		order by w.name asc, w.id asc
	`, strings.TrimSpace(orgUUID))
	if err != nil {
		if isUndefinedRelationError(err) {
			return []platform.ConsoleWorkspace{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var out []platform.ConsoleWorkspace
	for rows.Next() {
		var workspace platform.ConsoleWorkspace
		var dataResidencyBytes []byte
		var tagsBytes []byte
		if err := rows.Scan(
			&workspace.UUID,
			&workspace.OrgUUID,
			&workspace.Name,
			&workspace.DisplayColor,
			&workspace.Color,
			&dataResidencyBytes,
			&workspace.ExternalKeyID,
			&tagsBytes,
			&workspace.ArchivedAt,
			&workspace.CreatedAt,
			&workspace.UpdatedAt,
		); err != nil {
			return nil, mapNoRows(err)
		}
		dataResidency, settings, err := parseConsoleWorkspaceDataResidencyJSON(dataResidencyBytes)
		if err != nil {
			return nil, err
		}
		workspace.DataResidency = dataResidency
		workspace.DataResidencySettings = settings
		tags, err := parseConsoleWorkspaceTagsJSON(tagsBytes)
		if err != nil {
			return nil, err
		}
		workspace.Tags = tags
		out = append(out, workspace)
	}
	return out, rows.Err()
}

func scanConsoleWorkspace(row consoleAPIKeyScanner) (platform.ConsoleWorkspace, error) {
	var workspace platform.ConsoleWorkspace
	var dataResidencyBytes []byte
	var tagsBytes []byte
	if err := row.Scan(
		&workspace.UUID,
		&workspace.OrgUUID,
		&workspace.Name,
		&workspace.DisplayColor,
		&workspace.Color,
		&dataResidencyBytes,
		&workspace.ExternalKeyID,
		&tagsBytes,
		&workspace.ArchivedAt,
		&workspace.CreatedAt,
		&workspace.UpdatedAt,
	); err != nil {
		return platform.ConsoleWorkspace{}, mapNoRows(err)
	}
	dataResidency, settings, err := parseConsoleWorkspaceDataResidencyJSON(dataResidencyBytes)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	workspace.DataResidency = dataResidency
	workspace.DataResidencySettings = settings
	tags, err := parseConsoleWorkspaceTagsJSON(tagsBytes)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	workspace.Tags = tags
	return workspace, nil
}

func consoleWorkspaceDataResidencyJSON(dataResidency *string) ([]byte, error) {
	workspaceGeo := ""
	if dataResidency != nil {
		workspaceGeo = strings.TrimSpace(*dataResidency)
	}
	if workspaceGeo == "" {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]string{
		"workspace_geo":          workspaceGeo,
		"allowed_inference_geos": "unrestricted",
		"default_inference_geo":  "global",
	})
}

func parseConsoleWorkspaceDataResidencyJSON(raw []byte) (*string, *platform.ConsoleWorkspaceDataResidency, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, nil, err
	}
	settings := &platform.ConsoleWorkspaceDataResidency{}
	switch typed := value.(type) {
	case string:
		settings.WorkspaceGeo = strings.TrimSpace(typed)
	case map[string]any:
		settings.WorkspaceGeo = stringValueFromMap(typed, "workspace_geo")
		settings.AllowedInferenceGeos = stringValueFromMap(typed, "allowed_inference_geos")
		settings.DefaultInferenceGeo = stringValueFromMap(typed, "default_inference_geo")
	}
	if strings.TrimSpace(settings.WorkspaceGeo) == "" &&
		strings.TrimSpace(settings.AllowedInferenceGeos) == "" &&
		strings.TrimSpace(settings.DefaultInferenceGeo) == "" {
		return nil, nil, nil
	}
	var dataResidency *string
	if workspaceGeo := strings.TrimSpace(settings.WorkspaceGeo); workspaceGeo != "" {
		dataResidency = &workspaceGeo
	}
	return dataResidency, settings, nil
}

func stringValueFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func parseConsoleWorkspaceTagsJSON(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	tags := map[string]string{}
	if err := json.Unmarshal(raw, &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func consolePrefixedID(prefix string, bytes int) string {
	return prefix + "_" + consoleRandomToken(bytes)
}

func consoleRandomToken(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
