package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type bootstrapUserContextRow struct {
	UserExternalID string `db:"user_external_id"`
	OrgUUID        string `db:"org_uuid"`
}

type bootstrapUserRow struct {
	UUID          string    `db:"uuid"`
	ExternalID    string    `db:"external_id"`
	Email         string    `db:"email"`
	FullName      *string   `db:"full_name"`
	DisplayName   *string   `db:"display_name"`
	IsVerified    bool      `db:"is_verified"`
	AgeIsVerified bool      `db:"age_is_verified"`
	CreatedAt     time.Time `db:"created_at"`
}

type bootstrapOrganizationRow struct {
	UUID                   string    `db:"uuid"`
	ExternalID             string    `db:"external_id"`
	Name                   string    `db:"name"`
	Domain                 *string   `db:"domain"`
	ParentOrganizationUUID *string   `db:"parent_organization_uuid"`
	Settings               []byte    `db:"settings"`
	CreatedAt              time.Time `db:"created_at"`
	UpdatedAt              time.Time `db:"updated_at"`
	Role                   string    `db:"role"`
	AddedAt                time.Time `db:"added_at"`
}

func (d *DB) FindBootstrapUserContext(ctx context.Context, preferredOrgUUID string) (string, string, error) {
	if d == nil || d.sql == nil {
		return "", "", platform.ErrNotFound
	}

	query := `
		select
			u.external_id as user_external_id,
			cast(o.uuid as text) as org_uuid
		from users u
		join organizations o on o.id = u.organization_id
		where u.deleted_at is null
	`
	arguments := map[string]any{}
	if trimmedPreferredOrgUUID := strings.TrimSpace(preferredOrgUUID); trimmedPreferredOrgUUID != "" {
		query += ` and (cast(o.uuid as text) = :preferred_org_uuid or o.external_id = :preferred_org_uuid)`
		arguments["preferred_org_uuid"] = trimmedPreferredOrgUUID
	}
	query += `
		order by case when u.external_id = 'user_default' then 0 else 1 end, u.added_at asc, u.id asc
		limit 1
	`

	var row bootstrapUserContextRow
	if err := namedGetContext(ctx, d.sql, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", platform.ErrNotFound
		}
		return "", "", mapNoRows(err)
	}
	return row.UserExternalID, row.OrgUUID, nil
}

func (d *DB) GetBootstrapUser(ctx context.Context, userExternalID string) (*platform.UserRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(userExternalID) == "" {
		return nil, platform.ErrNotFound
	}

	var row bootstrapUserRow
	err := namedGetContext(ctx, d.sql, &row, `
		select
			cast(u.uuid as text) as uuid,
			u.external_id,
			u.email,
			nullif(u.name, '') as full_name,
			nullif(u.name, '') as display_name,
			true as is_verified,
			true as age_is_verified,
			u.added_at as created_at
		from users u
		where u.deleted_at is null
		  and (
			u.external_id = :user_external_id
			or cast(u.uuid as text) = :user_external_id
			or 'user_' || left(replace(cast(u.uuid as text), '-', ''), 24) = :user_external_id
		  )
		order by u.added_at asc, u.id asc
		limit 1
	`, map[string]any{"user_external_id": strings.TrimSpace(userExternalID)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, mapNoRows(err)
	}

	user := &platform.UserRecord{
		UUID:          row.UUID,
		ExternalID:    row.ExternalID,
		Email:         row.Email,
		FullName:      row.FullName,
		DisplayName:   row.DisplayName,
		IsVerified:    row.IsVerified,
		AgeIsVerified: row.AgeIsVerified,
		Settings:      map[string]any{},
		CreatedAt:     row.CreatedAt,
	}
	return user, nil
}

func (d *DB) GetPlatformOrganization(ctx context.Context, orgUUID string) (*platform.OrganizationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, platform.ErrNotFound
	}

	row, err := getBootstrapOrganizationRow(ctx, d.sql, strings.TrimSpace(orgUUID))
	if err != nil {
		return nil, err
	}
	org, err := row.organizationRecord()
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) UpdatePlatformOrganization(ctx context.Context, orgUUID string, patch platform.OrganizationUpdatePatch) (*platform.OrganizationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, platform.ErrNotFound
	}
	current, err := d.GetPlatformOrganization(ctx, orgUUID)
	if err != nil {
		return nil, err
	}
	var name any
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
	}
	var settingsValue any
	if patch.Settings != nil {
		settings := cloneOrganizationSettings(current.Settings)
		mergeOrganizationSettings(settings, patch.Settings)
		settingsBytes, err := json.Marshal(settings)
		if err != nil {
			return nil, err
		}
		settingsValue = string(settingsBytes)
	}

	var row bootstrapOrganizationRow
	err = namedGetContext(ctx, d.sql, &row, `
		update organizations
		set name = coalesce(CAST(:name AS text), name),
		    settings = coalesce(CAST(:settings AS jsonb), settings),
		    updated_at = current_timestamp
		where cast(uuid as text) = :org_uuid or external_id = :org_uuid
		returning
			cast(uuid as text) as uuid,
			external_id,
			name,
			CAST(NULL AS text) as domain,
			CAST(NULL AS text) as parent_organization_uuid,
			coalesce(settings, CAST('{}' AS jsonb)) as settings,
			created_at,
			updated_at,
			'' as role,
			created_at as added_at
	`, map[string]any{
		"org_uuid": strings.TrimSpace(orgUUID),
		"name":     name,
		"settings": settingsValue,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, mapNoRows(err)
	}
	org, err := row.organizationRecord()
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) ListBootstrapUserOrganizations(ctx context.Context, userExternalID string, preferredOrgUUID string) ([]platform.UserOrganizationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(userExternalID) == "" {
		return []platform.UserOrganizationRecord{}, nil
	}

	rows := []bootstrapOrganizationRow{}
	err := namedSelectContext(ctx, d.sql, &rows, `
		select
			cast(o.uuid as text) as uuid,
			o.external_id,
			o.name,
			CAST(NULL AS text) as domain,
			CAST(NULL AS text) as parent_organization_uuid,
			coalesce(o.settings, CAST('{}' AS jsonb)) as settings,
			o.created_at,
			o.updated_at,
			u.role,
			u.added_at
		from users u
		join organizations o on o.id = u.organization_id
		where u.deleted_at is null
		  and (
			u.external_id = :user_external_id
			or cast(u.uuid as text) = :user_external_id
			or 'user_' || left(replace(cast(u.uuid as text), '-', ''), 24) = :user_external_id
		  )
		order by
			case when cast(o.uuid as text) = :preferred_org_uuid or o.external_id = :preferred_org_uuid then 0 else 1 end,
			u.added_at asc,
			u.id asc
	`, map[string]any{
		"user_external_id":   strings.TrimSpace(userExternalID),
		"preferred_org_uuid": strings.TrimSpace(preferredOrgUUID),
	})
	if err != nil {
		return nil, err
	}

	out := make([]platform.UserOrganizationRecord, 0, len(rows))
	for _, row := range rows {
		org, err := row.userOrganizationRecord()
		if err != nil {
			return nil, err
		}
		out = append(out, org)
	}
	return out, nil
}

func (d *DB) GetOrganizationProfile(ctx context.Context, orgUUID string) (platform.OrganizationProfile, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	var profileBytes []byte
	err := namedGetContext(ctx, d.sql, &profileBytes, `
		select coalesce(profile, CAST('{}' AS jsonb))
		from organizations
		where cast(uuid as text) = :org_uuid or external_id = :org_uuid
		limit 1
	`, map[string]any{"org_uuid": strings.TrimSpace(orgUUID)})
	if errors.Is(err, sql.ErrNoRows) {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	if err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(profileBytes)
}

func (d *DB) UpdateOrganizationProfile(ctx context.Context, orgUUID string, profile platform.OrganizationProfile) (platform.OrganizationProfile, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		return platform.OrganizationProfile{}, err
	}
	var savedBytes []byte
	err = namedGetContext(ctx, d.sql, &savedBytes, `
		update organizations
		set profile = CAST(:profile AS jsonb),
		    updated_at = current_timestamp
		where cast(uuid as text) = :org_uuid or external_id = :org_uuid
		returning coalesce(profile, CAST('{}' AS jsonb))
	`, map[string]any{
		"org_uuid": strings.TrimSpace(orgUUID),
		"profile":  string(profileBytes),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	if err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(savedBytes)
}

func getBootstrapOrganizationRow(ctx context.Context, database sqlxNamedQueryer, orgUUID string) (bootstrapOrganizationRow, error) {
	var row bootstrapOrganizationRow
	err := namedGetContext(ctx, database, &row, `
		select
			cast(o.uuid as text) as uuid,
			o.external_id,
			o.name,
			CAST(NULL AS text) as domain,
			CAST(NULL AS text) as parent_organization_uuid,
			coalesce(o.settings, CAST('{}' AS jsonb)) as settings,
			o.created_at,
			o.updated_at,
			'' as role,
			o.created_at as added_at
		from organizations o
		where cast(o.uuid as text) = :org_uuid or o.external_id = :org_uuid
		limit 1
	`, map[string]any{"org_uuid": orgUUID})
	if errors.Is(err, sql.ErrNoRows) {
		return bootstrapOrganizationRow{}, platform.ErrNotFound
	}
	if err != nil {
		return bootstrapOrganizationRow{}, mapNoRows(err)
	}
	return row, nil
}

func (row bootstrapOrganizationRow) organizationRecord() (*platform.OrganizationRecord, error) {
	settings, err := decodeOrganizationSettings(row.Settings)
	if err != nil {
		return nil, err
	}
	return &platform.OrganizationRecord{
		UUID:                   row.UUID,
		ExternalID:             row.ExternalID,
		Name:                   row.Name,
		Domain:                 row.Domain,
		ParentOrganizationUUID: row.ParentOrganizationUUID,
		Settings:               settings,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}, nil
}

func (row bootstrapOrganizationRow) userOrganizationRecord() (platform.UserOrganizationRecord, error) {
	organization, err := row.organizationRecord()
	if err != nil {
		return platform.UserOrganizationRecord{}, err
	}
	return platform.UserOrganizationRecord{
		OrganizationRecord: *organization,
		Role:               row.Role,
		AddedAt:            row.AddedAt,
	}, nil
}

func decodeOrganizationSettings(raw []byte) (map[string]any, error) {
	settings := map[string]any{}
	if len(raw) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

func decodeOrganizationProfile(raw []byte) (platform.OrganizationProfile, error) {
	profile := platform.OrganizationProfile{}
	if len(raw) == 0 {
		return profile, nil
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		return platform.OrganizationProfile{}, err
	}
	return profile, nil
}

func cloneOrganizationSettings(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		if typed, ok := item.(map[string]any); ok {
			out[key] = cloneOrganizationSettings(typed)
			continue
		}
		out[key] = item
	}
	return out
}

func mergeOrganizationSettings(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if typedValue, ok := value.(map[string]any); ok {
			if typedDst, ok := dst[key].(map[string]any); ok {
				mergeOrganizationSettings(typedDst, typedValue)
				continue
			}
			dst[key] = cloneOrganizationSettings(typedValue)
			continue
		}
		dst[key] = value
	}
}
