package db

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func (d *DB) ListConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID *string) ([]platform.ConsoleAPIKey, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleAPIKey{}, nil
	}
	if workspaceID != nil {
		resolvedWorkspaceUUID, _, err := d.resolveConsoleAPIKeyCoreRefs(ctx, d.sql, orgUUID, *workspaceID, nil)
		if err != nil {
			return nil, err
		}
		workspaceID = &resolvedWorkspaceUUID
	}
	query, arguments := listConsoleAPIKeysQuery(orgUUID, workspaceID)
	keys, err := selectConsoleAPIKeysSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.ConsoleAPIKey{}, nil
		}
		return nil, err
	}
	return keys, nil
}

func listConsoleAPIKeysQuery(orgUUID string, workspaceID *string) (string, map[string]any) {
	query := `
		select
			` + consoleAPIKeySQLXColumns + `
		from console_api_keys
		where organization_uuid = CAST(:organization_uuid AS uuid)
	`
	arguments := map[string]any{"organization_uuid": strings.TrimSpace(orgUUID)}
	if workspaceID != nil {
		query += ` and workspace_uuid = CAST(:workspace_uuid AS uuid)`
		arguments["workspace_uuid"] = strings.TrimSpace(*workspaceID)
	}
	query += ` order by created_at desc, external_id desc`
	return query, arguments
}

func (d *DB) CreateConsoleAPIKey(ctx context.Context, input platform.CreateConsoleAPIKeyInput) (platform.CreateConsoleAPIKeyResult, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(input.OrgUUID) == "" || strings.TrimSpace(input.WorkspaceID) == "" {
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
	coreAPIKeyUUID := uuid.NewString()
	keyHash := auth.HashAPIKey(rawKey)

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	coreWorkspaceUUID, coreUserUUID, err := d.resolveConsoleAPIKeyCoreRefs(ctx, tx, orgUUID, workspaceID, input.CreatedByUserUUID)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}

	key, err := getConsoleAPIKeySQLX(ctx, tx, `
		insert into console_api_keys (
			external_id,
			api_key_ref_uuid,
			organization_uuid,
			workspace_uuid,
			workspace_id,
			name,
			key_prefix,
			key_suffix,
			key_hash,
			status,
			created_by_user_ref_uuid,
			expires_at
		)
		values (
			:external_id, CAST(:api_key_ref_uuid AS uuid),
			CAST(:organization_uuid AS uuid), CAST(:workspace_uuid AS uuid),
			:workspace_id, :name,
			:key_prefix, :key_suffix, :key_hash, 'active',
			CAST(:created_by_user_ref_uuid AS uuid), :expires_at
		)
		returning `+consoleAPIKeySQLXColumns+`
	`, map[string]any{
		"external_id":              externalID,
		"api_key_ref_uuid":         coreAPIKeyUUID,
		"organization_uuid":        orgUUID,
		"workspace_uuid":           coreWorkspaceUUID,
		"workspace_id":             workspaceID,
		"name":                     strings.TrimSpace(input.Name),
		"key_prefix":               keyPrefix,
		"key_suffix":               keySuffix,
		"key_hash":                 keyHash,
		"created_by_user_ref_uuid": coreUserUUID,
		"expires_at":               input.ExpiresAt,
	})
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		insert into api_keys (
			uuid,
			external_id,
			workspace_uuid,
			key_hash,
			status,
			created_by_user_uuid,
			name,
			partial_key_hint,
			expires_at
		)
		values (
			CAST(:uuid AS uuid), :external_id, CAST(:workspace_uuid AS uuid), :key_hash, 'active',
			CAST(:created_by_user_uuid AS uuid),
			:name, :partial_key_hint, :expires_at
		)
	`, map[string]any{
		"uuid":                 coreAPIKeyUUID,
		"external_id":          externalID,
		"workspace_uuid":       coreWorkspaceUUID,
		"key_hash":             keyHash,
		"created_by_user_uuid": coreUserUUID,
		"name":                 strings.TrimSpace(input.Name),
		"partial_key_hint":     partialAPIKeyHint(rawKey),
		"expires_at":           input.ExpiresAt,
	}); err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	return platform.CreateConsoleAPIKeyResult{
		APIKey: key,
		RawKey: rawKey,
	}, nil
}

func (d *DB) UpdateConsoleAPIKeyStatus(ctx context.Context, input platform.UpdateConsoleAPIKeyStatusInput) (platform.ConsoleAPIKey, error) {
	if d == nil || d.sql == nil ||
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

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	defer func() { _ = tx.Rollback() }()

	coreWorkspaceUUID, _, err := d.resolveConsoleAPIKeyCoreRefs(ctx, tx, orgUUID, workspaceID, nil)
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	key, err := getConsoleAPIKeySQLX(ctx, tx, `
		update console_api_keys
		set status = :status,
			archived_at = case
				when :status = 'archived' then coalesce(archived_at, now())
				else null
			end,
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
		  and workspace_uuid = CAST(:workspace_uuid AS uuid)
		  and external_id = :api_key_id
		returning `+consoleAPIKeySQLXColumns+`
	`, map[string]any{
		"organization_uuid": orgUUID,
		"workspace_uuid":    coreWorkspaceUUID,
		"api_key_id":        apiKeyID,
		"status":            status,
	})
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}

	if _, err := namedExecContext(ctx, tx, `
		update api_keys
		set status = :status,
			updated_at = now()
		where external_id = :api_key_id
	`, map[string]any{"api_key_id": key.ID, "status": status}); err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	return key, nil
}

func (d *DB) resolveConsoleAPIKeyCoreRefs(
	ctx context.Context,
	database sqlxNamedQueryer,
	orgUUID string,
	workspaceID string,
	userUUID *string,
) (string, *string, error) {
	coreWorkspaceUUID, err := resolveCompatibilityWorkspaceUUID(ctx, database, orgUUID, workspaceID)
	if err != nil {
		return "", nil, err
	}

	var coreUserUUID *string
	if userUUID != nil && strings.TrimSpace(*userUUID) != "" {
		var resolvedUserUUID string
		err := namedGetContext(ctx, database, &resolvedUserUUID, `
			select CAST(u.uuid AS text)
			from users u
			where u.organization_uuid = CAST(:org_uuid AS uuid)
			  and u.deleted_at is null
			  and (
				u.external_id = :user_uuid
				or CAST(u.uuid AS text) = :user_uuid
				or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_uuid
			  )
			limit 1
		`, map[string]any{"org_uuid": orgUUID, "user_uuid": strings.TrimSpace(*userUUID)})
		if err != nil && !errors.Is(mapNoRows(err), platform.ErrNotFound) {
			return "", nil, err
		}
		if err == nil {
			coreUserUUID = &resolvedUserUUID
		}
	}
	return coreWorkspaceUUID, coreUserUUID, nil
}

func resolveCompatibilityWorkspaceUUID(
	ctx context.Context,
	database sqlxNamedQueryer,
	orgUUID string,
	workspaceID string,
) (string, error) {
	var workspaceUUID string
	query := `
		select CAST(uuid AS text)
		from workspaces
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and archived_at is null
	`
	arguments := map[string]any{
		"organization_uuid": strings.TrimSpace(orgUUID),
		"workspace_id":      strings.TrimSpace(workspaceID),
	}
	if strings.TrimSpace(workspaceID) == "default" {
		query += `
			order by
				case
					when external_id = 'workspace_default' then 0
					when lower(name) = 'default' then 1
					else 2
				end,
				created_at asc,
				uuid asc
			limit 1
		`
	} else {
		query += `
			and (external_id = :workspace_id or CAST(uuid AS text) = :workspace_id)
			limit 1
		`
	}
	if err := namedGetContext(ctx, database, &workspaceUUID, query, arguments); err != nil {
		return "", mapNoRows(err)
	}
	return workspaceUUID, nil
}

func (d *DB) CountConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceID string) (int, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceID) == "" {
		return 0, nil
	}
	coreWorkspaceUUID, _, err := d.resolveConsoleAPIKeyCoreRefs(ctx, d.sql, orgUUID, workspaceID, nil)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var count int
	err = namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from console_api_keys
		where organization_uuid = CAST(:organization_uuid AS uuid)
		  and workspace_uuid = CAST(:workspace_uuid AS uuid)
		  and archived_at is null
	`, map[string]any{
		"organization_uuid": strings.TrimSpace(orgUUID),
		"workspace_uuid":    coreWorkspaceUUID,
	})
	if isUndefinedTableError(err) {
		return 0, nil
	}
	return count, err
}

func (d *DB) CreateConsoleWorkspace(ctx context.Context, input platform.CreateConsoleWorkspaceInput) (platform.ConsoleWorkspace, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(input.OrgUUID) == "" || strings.TrimSpace(input.Name) == "" {
		return platform.ConsoleWorkspace{}, platform.ErrNotFound
	}
	externalID := consolePrefixedID("wrkspc", 18)
	dataResidency, err := consoleWorkspaceDataResidencyJSON(input.DataResidency)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	workspace, err := getConsoleWorkspaceSQLX(ctx, d.sql, `
		with org as (
			select uuid, CAST(uuid AS text) as org_uuid
			from organizations
			where CAST(uuid AS text) = :org_uuid
			limit 1
		)
		insert into workspaces (
			uuid,
			external_id,
			organization_uuid,
			name,
			compartment_id,
			display_color,
			data_residency,
			tags
		)
		select :uuid, :external_id, org.uuid, :name, :external_id, :display_color,
			CAST(:data_residency AS jsonb), CAST('{}' AS jsonb)
		from org
		on conflict (organization_uuid, name) do update set
			display_color = excluded.display_color,
			data_residency = excluded.data_residency,
			archived_at = null,
			updated_at = now()
		returning
			external_id AS uuid,
			(select org_uuid from org) AS org_uuid,
			name,
			display_color AS display_color,
			display_color AS color,
			data_residency,
			external_key_id,
			tags,
			archived_at,
			created_at,
			updated_at
	`, map[string]any{
		"org_uuid":       strings.TrimSpace(input.OrgUUID),
		"uuid":           uuid.NewString(),
		"external_id":    externalID,
		"name":           strings.TrimSpace(input.Name),
		"display_color":  firstNonEmpty(strings.TrimSpace(input.DisplayColor), strings.TrimSpace(input.Color), "#9B87F5"),
		"data_residency": dataResidency,
	})
	if isUniqueViolation(err) {
		return platform.ConsoleWorkspace{}, err
	}
	return workspace, err
}

func (d *DB) ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]platform.ConsoleWorkspace, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleWorkspace{}, nil
	}
	query, arguments := listConsoleWorkspacesQuery(orgUUID, includeArchived)
	workspaces, err := selectConsoleWorkspacesSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.ConsoleWorkspace{}, nil
		}
		return nil, err
	}
	return workspaces, nil
}

func listConsoleWorkspacesQuery(orgUUID string, includeArchived bool) (string, map[string]any) {
	archivedFilter := "and w.archived_at is null"
	if includeArchived {
		archivedFilter = ""
	}
	query := `
		select
			w.external_id AS uuid,
			CAST(w.organization_uuid AS text) AS org_uuid,
			w.name,
			w.display_color AS display_color,
			w.display_color AS color,
			w.data_residency,
			w.external_key_id,
			w.tags,
			w.archived_at,
			w.created_at,
			w.updated_at
		from workspaces w
		where w.organization_uuid = CAST(:org_uuid AS uuid)
		` + archivedFilter + `
		order by w.name asc, w.uuid asc
	`
	return query, map[string]any{"org_uuid": strings.TrimSpace(orgUUID)}
}

func (r consoleWorkspaceRow) workspace() (platform.ConsoleWorkspace, error) {
	dataResidency, settings, err := parseConsoleWorkspaceDataResidencyJSON(r.DataResidency)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	tags, err := parseConsoleWorkspaceTagsJSON(r.Tags)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	return platform.ConsoleWorkspace{
		UUID:                  r.UUID,
		OrgUUID:               r.OrgUUID,
		Name:                  r.Name,
		DisplayColor:          r.DisplayColor,
		Color:                 r.Color,
		DataResidency:         dataResidency,
		DataResidencySettings: settings,
		ExternalKeyID:         r.ExternalKeyID,
		Tags:                  tags,
		ArchivedAt:            r.ArchivedAt,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}, nil
}

const consoleAPIKeySQLXColumns = `external_id AS id,
	CAST(organization_uuid AS text) AS org_uuid, workspace_id, name,
	key_prefix, key_suffix, status,
	CAST(created_by_user_ref_uuid AS text) AS created_by_user_uuid,
	last_used_at, expires_at,
	archived_at, created_at, updated_at`

type consoleAPIKeyRow struct {
	ID                string     `db:"id"`
	OrgUUID           string     `db:"org_uuid"`
	WorkspaceID       string     `db:"workspace_id"`
	Name              string     `db:"name"`
	KeyPrefix         string     `db:"key_prefix"`
	KeySuffix         string     `db:"key_suffix"`
	Status            string     `db:"status"`
	CreatedByUserUUID *string    `db:"created_by_user_uuid"`
	LastUsedAt        *time.Time `db:"last_used_at"`
	ExpiresAt         *time.Time `db:"expires_at"`
	ArchivedAt        *time.Time `db:"archived_at"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
}

type consoleWorkspaceRow struct {
	UUID          string     `db:"uuid"`
	OrgUUID       string     `db:"org_uuid"`
	Name          string     `db:"name"`
	DisplayColor  string     `db:"display_color"`
	Color         string     `db:"color"`
	DataResidency []byte     `db:"data_residency"`
	ExternalKeyID *string    `db:"external_key_id"`
	Tags          []byte     `db:"tags"`
	ArchivedAt    *time.Time `db:"archived_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

func getConsoleAPIKeySQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (platform.ConsoleAPIKey, error) {
	var row consoleAPIKeyRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return platform.ConsoleAPIKey{}, mapNoRows(err)
	}
	return row.key(), nil
}

func selectConsoleAPIKeysSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]platform.ConsoleAPIKey, error) {
	var rows []consoleAPIKeyRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	keys := make([]platform.ConsoleAPIKey, len(rows))
	for index := range rows {
		keys[index] = rows[index].key()
	}
	return keys, nil
}

func getConsoleWorkspaceSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (platform.ConsoleWorkspace, error) {
	var row consoleWorkspaceRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return platform.ConsoleWorkspace{}, mapNoRows(err)
	}
	return row.workspace()
}

func selectConsoleWorkspacesSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]platform.ConsoleWorkspace, error) {
	var rows []consoleWorkspaceRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	workspaces := make([]platform.ConsoleWorkspace, 0, len(rows))
	for _, row := range rows {
		workspace, err := row.workspace()
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, nil
}

func (r consoleAPIKeyRow) key() platform.ConsoleAPIKey {
	return platform.ConsoleAPIKey{
		ID:                r.ID,
		OrgUUID:           r.OrgUUID,
		WorkspaceID:       r.WorkspaceID,
		Name:              r.Name,
		KeyPrefix:         r.KeyPrefix,
		KeySuffix:         r.KeySuffix,
		Status:            r.Status,
		CreatedByUserUUID: r.CreatedByUserUUID,
		LastUsedAt:        r.LastUsedAt,
		ExpiresAt:         r.ExpiresAt,
		ArchivedAt:        r.ArchivedAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
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

func isUndefinedTableError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
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
