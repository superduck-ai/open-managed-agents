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

func (d *DB) ListConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceUUID *string) ([]platform.ConsoleAPIKey, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleAPIKey{}, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return []platform.ConsoleAPIKey{}, nil
	}
	typedWorkspaceUUID, err := parseDBNullableUUID("workspace_uuid", workspaceUUID)
	if err != nil {
		return []platform.ConsoleAPIKey{}, nil
	}
	query, arguments := listConsoleAPIKeysQuery(typedOrgUUID, typedWorkspaceUUID)
	keys, err := selectConsoleAPIKeysSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.ConsoleAPIKey{}, nil
		}
		return nil, err
	}
	return keys, nil
}

func listConsoleAPIKeysQuery(orgUUID uuid.UUID, workspaceUUID uuid.NullUUID) (string, map[string]any) {
	query := `
		select
			` + consoleAPIKeySQLXColumns + `
		from console_api_keys
		where organization_uuid = :organization_uuid
	`
	arguments := map[string]any{"organization_uuid": orgUUID}
	if workspaceUUID.Valid {
		query += ` and workspace_uuid = :workspace_uuid`
		arguments["workspace_uuid"] = workspaceUUID.UUID
	}
	query += ` order by created_at desc, external_id desc`
	return query, arguments
}

func (d *DB) CreateConsoleAPIKey(ctx context.Context, input platform.CreateConsoleAPIKeyInput) (platform.CreateConsoleAPIKeyResult, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(input.OrgUUID) == "" || strings.TrimSpace(input.WorkspaceUUID) == "" {
		return platform.CreateConsoleAPIKeyResult{}, platform.ErrNotFound
	}
	orgUUID, err := parseDBUUID("organization_uuid", input.OrgUUID)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, platform.ErrNotFound
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", input.WorkspaceUUID)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, platform.ErrNotFound
	}
	workspaceDisplayID := firstNonEmpty(strings.TrimSpace(input.WorkspaceDisplayID), workspaceUUID.String())
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
	coreAPIKeyUUID := uuid.New()
	keyHash := auth.HashAPIKey(rawKey)

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	coreUserUUID, err := resolveConsoleAPIKeyUserUUID(ctx, tx, orgUUID, input.CreatedByUserUUID)
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}

	key, err := getConsoleAPIKeySQLX(ctx, tx, `
		insert into console_api_keys (
			external_id,
			api_key_ref_uuid,
			organization_uuid,
			workspace_uuid,
			workspace_display_id,
			name,
			key_prefix,
			key_suffix,
			key_hash,
			status,
			created_by_user_ref_uuid,
			expires_at
		)
		values (
			:external_id, :api_key_ref_uuid,
			:organization_uuid, :workspace_uuid,
			:workspace_display_id, :name,
			:key_prefix, :key_suffix, :key_hash, 'active',
			:created_by_user_ref_uuid, :expires_at
		)
		returning `+consoleAPIKeySQLXColumns+`
	`, map[string]any{
		"external_id":              externalID,
		"api_key_ref_uuid":         coreAPIKeyUUID,
		"organization_uuid":        orgUUID,
		"workspace_uuid":           workspaceUUID,
		"workspace_display_id":     workspaceDisplayID,
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
			:uuid, :external_id, :workspace_uuid, :key_hash, 'active',
			:created_by_user_uuid,
			:name, :partial_key_hint, :expires_at
		)
	`, map[string]any{
		"uuid":                 coreAPIKeyUUID,
		"external_id":          externalID,
		"workspace_uuid":       workspaceUUID,
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
		strings.TrimSpace(input.WorkspaceUUID) == "" ||
		strings.TrimSpace(input.APIKeyID) == "" ||
		strings.TrimSpace(input.Status) == "" {
		return platform.ConsoleAPIKey{}, platform.ErrNotFound
	}
	orgUUID, err := parseDBUUID("organization_uuid", input.OrgUUID)
	if err != nil {
		return platform.ConsoleAPIKey{}, platform.ErrNotFound
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", input.WorkspaceUUID)
	if err != nil {
		return platform.ConsoleAPIKey{}, platform.ErrNotFound
	}
	apiKeyID := strings.TrimSpace(input.APIKeyID)
	status := strings.TrimSpace(input.Status)

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return platform.ConsoleAPIKey{}, err
	}
	defer func() { _ = tx.Rollback() }()

	key, err := getConsoleAPIKeySQLX(ctx, tx, `
		update console_api_keys
		set status = :status,
			archived_at = case
				when :status = 'archived' then coalesce(archived_at, now())
				else null
			end,
			updated_at = now()
		where organization_uuid = :organization_uuid
		  and workspace_uuid = :workspace_uuid
		  and external_id = :api_key_id
		returning `+consoleAPIKeySQLXColumns+`
	`, map[string]any{
		"organization_uuid": orgUUID,
		"workspace_uuid":    workspaceUUID,
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

func resolveConsoleAPIKeyUserUUID(
	ctx context.Context,
	database sqlxNamedQueryer,
	orgUUID uuid.UUID,
	userUUID *string,
) (uuid.NullUUID, error) {
	var coreUserUUID uuid.NullUUID
	if userUUID != nil && strings.TrimSpace(*userUUID) != "" {
		var resolvedUserUUID uuid.UUID
		err := namedGetContext(ctx, database, &resolvedUserUUID, `
			select u.uuid
			from users u
			where u.organization_uuid = :org_uuid
			  and u.deleted_at is null
			  and (
				u.external_id = :user_uuid
				or u.uuid = :user_internal_uuid
				or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_uuid
			  )
			limit 1
		`, map[string]any{
			"org_uuid":           orgUUID,
			"user_uuid":          strings.TrimSpace(*userUUID),
			"user_internal_uuid": tryParseDBUUIDIdentifier(*userUUID),
		})
		if err != nil && !errors.Is(mapNoRows(err), platform.ErrNotFound) {
			return uuid.NullUUID{}, err
		}
		if err == nil {
			coreUserUUID = uuid.NullUUID{UUID: resolvedUserUUID, Valid: true}
		}
	}
	return coreUserUUID, nil
}

func (d *DB) CountConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceUUID string) (int, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceUUID) == "" {
		return 0, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return 0, nil
	}
	typedWorkspaceUUID, err := parseDBUUID("workspace_uuid", workspaceUUID)
	if err != nil {
		return 0, nil
	}
	var count int
	err = namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from console_api_keys
		where organization_uuid = :organization_uuid
		  and workspace_uuid = :workspace_uuid
		  and archived_at is null
	`, map[string]any{
		"organization_uuid": typedOrgUUID,
		"workspace_uuid":    typedWorkspaceUUID,
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
	orgUUID, err := parseDBUUID("organization_uuid", input.OrgUUID)
	if err != nil {
		return platform.ConsoleWorkspace{}, platform.ErrNotFound
	}
	workspace, err := getConsoleWorkspaceSQLX(ctx, d.sql, `
		with org as (
			select uuid
			from organizations
			where uuid = :org_uuid
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
			uuid,
			external_id,
			organization_uuid AS org_uuid,
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
		"org_uuid":       orgUUID,
		"uuid":           uuid.New(),
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
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return []platform.ConsoleWorkspace{}, nil
	}
	query, arguments := listConsoleWorkspacesQuery(typedOrgUUID, includeArchived)
	workspaces, err := selectConsoleWorkspacesSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.ConsoleWorkspace{}, nil
		}
		return nil, err
	}
	return workspaces, nil
}

func listConsoleWorkspacesQuery(orgUUID uuid.UUID, includeArchived bool) (string, map[string]any) {
	archivedFilter := "and w.archived_at is null"
	if includeArchived {
		archivedFilter = ""
	}
	query := `
		select
			w.uuid,
			w.external_id,
			w.organization_uuid AS org_uuid,
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
		where w.organization_uuid = :org_uuid
		` + archivedFilter + `
		order by w.name asc, w.uuid asc
	`
	return query, map[string]any{"org_uuid": orgUUID}
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
		UUID:                  r.UUID.String(),
		ExternalID:            r.ExternalID,
		OrgUUID:               r.OrgUUID.String(),
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
	organization_uuid AS org_uuid,
	workspace_uuid, workspace_display_id, name,
	key_prefix, key_suffix, status,
	created_by_user_ref_uuid AS created_by_user_uuid,
	last_used_at, expires_at,
	archived_at, created_at, updated_at`

type consoleAPIKeyRow struct {
	ID                 string        `db:"id"`
	OrgUUID            uuid.UUID     `db:"org_uuid"`
	WorkspaceUUID      uuid.UUID     `db:"workspace_uuid"`
	WorkspaceDisplayID string        `db:"workspace_display_id"`
	Name               string        `db:"name"`
	KeyPrefix          string        `db:"key_prefix"`
	KeySuffix          string        `db:"key_suffix"`
	Status             string        `db:"status"`
	CreatedByUserUUID  uuid.NullUUID `db:"created_by_user_uuid"`
	LastUsedAt         *time.Time    `db:"last_used_at"`
	ExpiresAt          *time.Time    `db:"expires_at"`
	ArchivedAt         *time.Time    `db:"archived_at"`
	CreatedAt          time.Time     `db:"created_at"`
	UpdatedAt          time.Time     `db:"updated_at"`
}

type consoleWorkspaceRow struct {
	UUID          uuid.UUID  `db:"uuid"`
	ExternalID    string     `db:"external_id"`
	OrgUUID       uuid.UUID  `db:"org_uuid"`
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
		ID:                 r.ID,
		OrgUUID:            r.OrgUUID.String(),
		WorkspaceUUID:      r.WorkspaceUUID.String(),
		WorkspaceDisplayID: r.WorkspaceDisplayID,
		Name:               r.Name,
		KeyPrefix:          r.KeyPrefix,
		KeySuffix:          r.KeySuffix,
		Status:             r.Status,
		CreatedByUserUUID:  nullableUUIDString(r.CreatedByUserUUID),
		LastUsedAt:         r.LastUsedAt,
		ExpiresAt:          r.ExpiresAt,
		ArchivedAt:         r.ArchivedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
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
