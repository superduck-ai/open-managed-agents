package db

import (
	"context"
	"errors"
	"slices"
	"time"
)

type AdminAPIKey struct {
	UUID                    string     `db:"uuid"`
	ExternalID              string     `db:"external_id"`
	WorkspaceUUID           string     `db:"workspace_uuid"`
	WorkspaceExternalID     string     `db:"workspace_external_id"`
	CreatedByUserUUID       *string    `db:"created_by_user_uuid"`
	CreatedByUserExternalID *string    `db:"created_by_user_external_id"`
	Name                    string     `db:"name"`
	PartialKeyHint          string     `db:"partial_key_hint"`
	Status                  string     `db:"status"`
	CreatedAt               time.Time  `db:"created_at"`
	UpdatedAt               time.Time  `db:"updated_at"`
	ExpiresAt               *time.Time `db:"expires_at"`
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
	mapper := NewAdminAPIKeyMapper(d.mapperDB)
	key, found, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	if err != nil {
		return AdminAPIKey{}, err
	}
	if !found {
		return AdminAPIKey{}, ErrNotFound
	}
	return key, nil
}

func (d *DB) ListAdminAPIKeysPage(ctx context.Context, params ListAdminAPIKeysParams) ([]AdminAPIKey, bool, error) {
	if params.AfterID != "" && params.BeforeID != "" {
		return nil, false, errors.New("after_id and before_id cannot be used together")
	}
	mapper := NewAdminAPIKeyMapper(d.mapperDB)
	var anchor *adminAPIKeyPageAnchor
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
	before := params.BeforeID != ""
	keys, err := mapper.ListPage(
		ctx,
		params.OrganizationUUID,
		params.WorkspaceExternalID,
		params.CreatedByUserExternalID,
		params.Status,
		anchor,
		before,
		params.Limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(keys) > params.Limit
	keys = trimAdminPage(keys, params.Limit)
	if before {
		slices.Reverse(keys)
	}
	return keys, hasMore, nil
}

func (d *DB) UpdateAdminAPIKey(ctx context.Context, organizationUUID, externalID string, setName bool, name string, setStatus bool, status string) error {
	mapper := NewAdminAPIKeyMapper(d.mapperDB)
	rowsAffected, err := mapper.UpdateByExternalID(
		ctx,
		organizationUUID,
		externalID,
		setName,
		name,
		setStatus,
		status,
	)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
