package db

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/superduck-ai/yourbatis"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleAPIKeyMapper -sql ./console_api_keys_mapper.xml -out ./console_api_keys_mapper.sqlmap.gen.go -dialect postgres

type insertConsoleAPIKeyQuery struct {
	ExternalID         string
	APIKeyUUID         uuid.UUID
	OrganizationUUID   string
	WorkspaceUUID      string
	WorkspaceDisplayID string
	Name               string
	KeyPrefix          string
	KeySuffix          string
	KeyHash            string
	CreatedByUserUUID  *string
	ExpiresAt          *time.Time
}

type updateConsoleAPIKeyStatusQuery struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
	Status           string
}

type ConsoleAPIKeyMapper interface {
	List(ctx context.Context, organizationUUID, workspaceUUID string) ([]consoleAPIKeyRow, error)
	CountUnarchived(ctx context.Context, organizationUUID, workspaceUUID string) (int64, error)
	CreatorExists(ctx context.Context, organizationUUID, userUUID string) (bool, error)
	Insert(ctx context.Context, params insertConsoleAPIKeyQuery) (consoleAPIKeyRow, error)
	UpdateStatus(ctx context.Context, params updateConsoleAPIKeyStatusQuery) (consoleAPIKeyRow, error)
}

func (d *DB) ListConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceUUID *string) ([]platform.ConsoleAPIKey, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" {
		return []platform.ConsoleAPIKey{}, nil
	}
	workspaceID := ""
	if workspaceUUID != nil {
		workspaceID = *workspaceUUID
	}
	mapper := NewConsoleAPIKeyMapper(d.mapperDB)
	rows, err := mapper.List(ctx, orgUUID, workspaceID)
	if err != nil {
		return nil, err
	}
	return lo.Map(rows, func(row consoleAPIKeyRow, _ int) platform.ConsoleAPIKey {
		return row.key()
	}), nil
}

func (d *DB) CreateConsoleAPIKey(ctx context.Context, input platform.CreateConsoleAPIKeyInput) (platform.CreateConsoleAPIKeyResult, error) {
	if d == nil || d.mapperDB == nil || input.OrgUUID == "" || input.WorkspaceUUID == "" {
		return platform.CreateConsoleAPIKeyResult{}, platform.ErrNotFound
	}
	workspaceDisplayID := input.WorkspaceDisplayID
	if workspaceDisplayID == "" {
		workspaceDisplayID = input.WorkspaceUUID
	}
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
	workspaceAPIKeyUUID := uuid.New()
	keyHash := auth.HashAPIKey(rawKey)

	var key platform.ConsoleAPIKey
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		consoleMapper := NewConsoleAPIKeyMapper(executor)
		adminMapper := NewAdminAPIKeyMapper(executor)
		createdByUserUUID, resolveErr := findConsoleAPIKeyCreatorUUID(ctx, consoleMapper, input.OrgUUID, input.CreatedByUserUUID)
		if resolveErr != nil {
			return resolveErr
		}
		row, insertErr := consoleMapper.Insert(ctx, insertConsoleAPIKeyQuery{
			ExternalID:         externalID,
			APIKeyUUID:         workspaceAPIKeyUUID,
			OrganizationUUID:   input.OrgUUID,
			WorkspaceUUID:      input.WorkspaceUUID,
			WorkspaceDisplayID: workspaceDisplayID,
			Name:               input.Name,
			KeyPrefix:          keyPrefix,
			KeySuffix:          keySuffix,
			KeyHash:            keyHash,
			CreatedByUserUUID:  createdByUserUUID,
			ExpiresAt:          input.ExpiresAt,
		})
		if insertErr != nil {
			return insertErr
		}
		if insertErr = adminMapper.Insert(ctx, insertAdminAPIKeyParams{
			UUID:              workspaceAPIKeyUUID,
			ExternalID:        externalID,
			WorkspaceUUID:     input.WorkspaceUUID,
			KeyHash:           keyHash,
			CreatedByUserUUID: createdByUserUUID,
			Name:              input.Name,
			PartialKeyHint:    partialAPIKeyHint(rawKey),
			ExpiresAt:         input.ExpiresAt,
		}); insertErr != nil {
			return insertErr
		}
		key = row.key()
		return nil
	})
	if err != nil {
		return platform.CreateConsoleAPIKeyResult{}, err
	}
	return platform.CreateConsoleAPIKeyResult{
		APIKey: key,
		RawKey: rawKey,
	}, nil
}

func (d *DB) UpdateConsoleAPIKeyStatus(ctx context.Context, input platform.UpdateConsoleAPIKeyStatusInput) (platform.ConsoleAPIKey, error) {
	if d == nil || d.mapperDB == nil ||
		input.OrgUUID == "" ||
		input.WorkspaceUUID == "" ||
		input.APIKeyID == "" ||
		input.Status == "" {
		return platform.ConsoleAPIKey{}, platform.ErrNotFound
	}

	var key platform.ConsoleAPIKey
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		consoleMapper := NewConsoleAPIKeyMapper(executor)
		adminMapper := NewAdminAPIKeyMapper(executor)
		row, updateErr := consoleMapper.UpdateStatus(ctx, updateConsoleAPIKeyStatusQuery{
			OrganizationUUID: input.OrgUUID,
			WorkspaceUUID:    input.WorkspaceUUID,
			ExternalID:       input.APIKeyID,
			Status:           input.Status,
		})
		if updateErr != nil {
			return mapNoRows(updateErr)
		}
		rowsAffected, updateErr := adminMapper.UpdateStatusByUUID(ctx, row.WorkspaceAPIKeyUUID, input.Status)
		if updateErr != nil {
			return updateErr
		}
		if rowsAffected != 1 {
			return fmt.Errorf("update workspace API key %q: affected %d rows, want 1", row.ID, rowsAffected)
		}
		key = row.key()
		return nil
	})
	return key, err
}

func findConsoleAPIKeyCreatorUUID(
	ctx context.Context,
	mapper ConsoleAPIKeyMapper,
	orgUUID string,
	userUUID *string,
) (*string, error) {
	if userUUID == nil || *userUUID == "" {
		return nil, nil
	}
	found, err := mapper.CreatorExists(ctx, orgUUID, *userUUID)
	if err != nil || !found {
		return nil, err
	}
	return userUUID, nil
}

func (d *DB) CountConsoleAPIKeys(ctx context.Context, orgUUID string, workspaceUUID string) (int, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" || workspaceUUID == "" {
		return 0, nil
	}
	mapper := NewConsoleAPIKeyMapper(d.mapperDB)
	count, err := mapper.CountUnarchived(ctx, orgUUID, workspaceUUID)
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func (d *DB) CreateConsoleWorkspace(ctx context.Context, input platform.CreateConsoleWorkspaceInput) (platform.ConsoleWorkspace, error) {
	if d == nil || d.sql == nil || input.OrgUUID == "" || input.Name == "" {
		return platform.ConsoleWorkspace{}, platform.ErrNotFound
	}
	externalID := consolePrefixedID("wrkspc", 18)
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
			tags
		)
		select :uuid, :external_id, org.uuid, :name, :external_id, :display_color,
			CAST('{}' AS jsonb)
		from org
		on conflict (organization_uuid, name) do update set
			display_color = excluded.display_color,
			archived_at = null,
			updated_at = now()
		returning
			uuid,
			external_id,
			organization_uuid AS org_uuid,
			name,
			display_color AS display_color,
			display_color AS color,
			external_key_id,
			tags,
			archived_at,
			created_at,
			updated_at
	`, map[string]any{
		"org_uuid":      orgUUID,
		"uuid":          uuid.New(),
		"external_id":   externalID,
		"name":          strings.TrimSpace(input.Name),
		"display_color": firstNonEmpty(strings.TrimSpace(input.DisplayColor), strings.TrimSpace(input.Color), "#9B87F5"),
	})
	if isUniqueViolation(err) {
		return platform.ConsoleWorkspace{}, err
	}
	return workspace, err
}

func (d *DB) ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]platform.ConsoleWorkspace, error) {
	if d == nil || d.sql == nil || orgUUID == "" {
		return []platform.ConsoleWorkspace{}, nil
	}
	query, arguments := listConsoleWorkspacesQuery(orgUUID, includeArchived)
	workspaces, err := selectConsoleWorkspacesSQLX(ctx, d.sql, query, arguments)
	if err != nil {
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
			w.uuid,
			w.external_id,
			w.organization_uuid AS org_uuid,
			w.name,
			w.display_color AS display_color,
			w.display_color AS color,
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
	tags, err := parseConsoleWorkspaceTagsJSON(r.Tags)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	return platform.ConsoleWorkspace{
		UUID:          r.UUID.String(),
		ExternalID:    r.ExternalID,
		OrgUUID:       r.OrgUUID.String(),
		Name:          r.Name,
		DisplayColor:  r.DisplayColor,
		Color:         r.Color,
		ExternalKeyID: r.ExternalKeyID,
		Tags:          tags,
		ArchivedAt:    r.ArchivedAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}, nil
}

type consoleAPIKeyRow struct {
	ID                  string        `db:"id"`
	WorkspaceAPIKeyUUID uuid.UUID     `db:"workspace_api_key_uuid"`
	OrgUUID             uuid.UUID     `db:"org_uuid"`
	WorkspaceUUID       uuid.UUID     `db:"workspace_uuid"`
	WorkspaceDisplayID  string        `db:"workspace_display_id"`
	Name                string        `db:"name"`
	KeyPrefix           string        `db:"key_prefix"`
	KeySuffix           string        `db:"key_suffix"`
	Status              string        `db:"status"`
	CreatedByUserUUID   uuid.NullUUID `db:"created_by_user_uuid"`
	LastUsedAt          *time.Time    `db:"last_used_at"`
	ExpiresAt           *time.Time    `db:"expires_at"`
	ArchivedAt          *time.Time    `db:"archived_at"`
	CreatedAt           time.Time     `db:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at"`
}

type consoleWorkspaceRow struct {
	UUID          uuid.UUID  `db:"uuid"`
	ExternalID    string     `db:"external_id"`
	OrgUUID       uuid.UUID  `db:"org_uuid"`
	Name          string     `db:"name"`
	DisplayColor  string     `db:"display_color"`
	Color         string     `db:"color"`
	ExternalKeyID *string    `db:"external_key_id"`
	Tags          []byte     `db:"tags"`
	ArchivedAt    *time.Time `db:"archived_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
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
