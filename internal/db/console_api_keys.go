package db

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/superduck-ai/yourbatis"
)

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
	workspaceAPIKeyUUID := uuid.NewString()
	keyHash := auth.HashAPIKey(rawKey)

	var key platform.ConsoleAPIKey
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		consoleMapper := NewConsoleAPIKeyMapper(executor)
		userMapper := NewConsoleUserMapper(executor)
		adminMapper := NewAdminAPIKeyMapper(executor)
		createdByUserUUID, resolveErr := findConsoleAPIKeyCreatorUUID(ctx, userMapper, input.OrgUUID, input.CreatedByUserUUID)
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
	mapper ConsoleUserMapper,
	orgUUID string,
	userUUID *string,
) (*string, error) {
	if userUUID == nil || *userUUID == "" {
		return nil, nil
	}
	found, err := mapper.ExistsActiveByUUID(ctx, orgUUID, *userUUID)
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
	if d == nil || d.mapperDB == nil || input.OrgUUID == "" || input.Name == "" {
		return platform.ConsoleWorkspace{}, platform.ErrNotFound
	}
	externalID := consolePrefixedID("wrkspc", 18)
	displayColor := input.DisplayColor
	if displayColor == "" {
		displayColor = input.Color
	}
	if displayColor == "" {
		displayColor = "#9B87F5"
	}
	mapper := NewConsoleWorkspaceMapper(d.mapperDB)
	row, err := mapper.Upsert(ctx, upsertConsoleWorkspaceParams{
		UUID:         uuid.NewString(),
		ExternalID:   externalID,
		OrgUUID:      input.OrgUUID,
		Name:         strings.TrimSpace(input.Name),
		DisplayColor: displayColor,
	})
	if isUniqueViolation(err) {
		return platform.ConsoleWorkspace{}, err
	}
	if err != nil {
		return platform.ConsoleWorkspace{}, mapNoRows(err)
	}
	return row.workspace()
}

func (d *DB) ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]platform.ConsoleWorkspace, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" {
		return []platform.ConsoleWorkspace{}, nil
	}
	mapper := NewConsoleWorkspaceMapper(d.mapperDB)
	rows, err := mapper.List(ctx, orgUUID, includeArchived)
	if err != nil {
		return nil, err
	}
	workspaces := make([]platform.ConsoleWorkspace, 0, len(rows))
	for _, row := range rows {
		workspace, mapErr := row.workspace()
		if mapErr != nil {
			return nil, mapErr
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, nil
}

func (r consoleWorkspaceRow) workspace() (platform.ConsoleWorkspace, error) {
	tags, err := parseConsoleWorkspaceTagsJSON(r.Tags)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	return platform.ConsoleWorkspace{
		UUID:          r.UUID,
		ExternalID:    r.ExternalID,
		OrgUUID:       r.OrgUUID,
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
func (r consoleAPIKeyRow) key() platform.ConsoleAPIKey {
	var createdByUserUUID *string
	if r.CreatedByUserUUID.Valid {
		value := r.CreatedByUserUUID.String
		createdByUserUUID = &value
	}
	return platform.ConsoleAPIKey{
		ID:                 r.ID,
		OrgUUID:            r.OrgUUID,
		WorkspaceUUID:      r.WorkspaceUUID,
		WorkspaceDisplayID: r.WorkspaceDisplayID,
		Name:               r.Name,
		KeyPrefix:          r.KeyPrefix,
		KeySuffix:          r.KeySuffix,
		Status:             r.Status,
		CreatedByUserUUID:  createdByUserUUID,
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
