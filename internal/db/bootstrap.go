package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func (d *DB) FindBootstrapUserContext(ctx context.Context, preferredOrgUUID string) (string, string, error) {
	if d == nil || d.Pool == nil {
		return "", "", platform.ErrNotFound
	}
	query := `
		select u.external_id, o.uuid::text
		from users u
		join organizations o on o.id = u.organization_id
		where u.deleted_at is null
	`
	args := []any{}
	if strings.TrimSpace(preferredOrgUUID) != "" {
		query += ` and (o.uuid::text = $1 or o.external_id = $1)`
		args = append(args, strings.TrimSpace(preferredOrgUUID))
	}
	query += `
		order by case when u.external_id = 'user_default' then 0 else 1 end, u.added_at asc, u.id asc
		limit 1
	`
	var userExternalID string
	var orgUUID string
	if err := d.Pool.QueryRow(ctx, query, args...).Scan(&userExternalID, &orgUUID); err != nil {
		return "", "", mapNoRows(err)
	}
	return userExternalID, orgUUID, nil
}

func (d *DB) GetBootstrapUser(ctx context.Context, userExternalID string) (*platform.UserRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(userExternalID) == "" {
		return nil, platform.ErrNotFound
	}
	var user platform.UserRecord
	if err := d.Pool.QueryRow(ctx, `
		select
			u.uuid::text,
			u.external_id,
			u.email,
			nullif(u.name, ''),
			nullif(u.name, ''),
			true,
			true,
			u.added_at
		from users u
		where u.deleted_at is null
		  and (
			u.external_id = $1
			or u.uuid::text = $1
			or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $1
		  )
		order by u.added_at asc, u.id asc
		limit 1
	`, strings.TrimSpace(userExternalID)).Scan(
		&user.UUID,
		&user.ExternalID,
		&user.Email,
		&user.FullName,
		&user.DisplayName,
		&user.IsVerified,
		&user.AgeIsVerified,
		&user.CreatedAt,
	); err != nil {
		return nil, mapNoRows(err)
	}
	user.Settings = map[string]any{}
	return &user, nil
}

func (d *DB) GetPlatformOrganization(ctx context.Context, orgUUID string) (*platform.OrganizationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var org platform.OrganizationRecord
	settingsBytes := []byte{}
	if err := d.Pool.QueryRow(ctx, `
		select
			o.uuid::text,
			o.external_id,
			o.name,
			null::text,
			null::text,
			coalesce(o.settings, '{}'::jsonb),
			o.created_at,
			o.updated_at
		from organizations o
		where o.uuid::text = $1 or o.external_id = $1
		limit 1
	`, strings.TrimSpace(orgUUID)).Scan(
		&org.UUID,
		&org.ExternalID,
		&org.Name,
		&org.Domain,
		&org.ParentOrganizationUUID,
		&settingsBytes,
		&org.CreatedAt,
		&org.UpdatedAt,
	); err != nil {
		return nil, mapNoRows(err)
	}
	settings, err := decodeOrganizationSettings(settingsBytes)
	if err != nil {
		return nil, err
	}
	org.Settings = settings
	return &org, nil
}

func (d *DB) UpdatePlatformOrganization(ctx context.Context, orgUUID string, patch platform.OrganizationUpdatePatch) (*platform.OrganizationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
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

	var org platform.OrganizationRecord
	settingsBytes := []byte{}
	if err := d.Pool.QueryRow(ctx, `
		update organizations
		set name = coalesce($2::text, name),
		    settings = coalesce($3::jsonb, settings),
		    updated_at = current_timestamp
		where uuid::text = $1 or external_id = $1
		returning uuid::text, external_id, name, null::text, null::text, coalesce(settings, '{}'::jsonb), created_at, updated_at
	`, strings.TrimSpace(orgUUID), name, settingsValue).Scan(
		&org.UUID,
		&org.ExternalID,
		&org.Name,
		&org.Domain,
		&org.ParentOrganizationUUID,
		&settingsBytes,
		&org.CreatedAt,
		&org.UpdatedAt,
	); err != nil {
		return nil, mapNoRows(err)
	}
	settings, err := decodeOrganizationSettings(settingsBytes)
	if err != nil {
		return nil, err
	}
	org.Settings = settings
	return &org, nil
}

func (d *DB) ListBootstrapUserOrganizations(ctx context.Context, userExternalID string, preferredOrgUUID string) ([]platform.UserOrganizationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(userExternalID) == "" {
		return []platform.UserOrganizationRecord{}, nil
	}
	rows, err := d.Pool.Query(ctx, `
		select
			o.uuid::text,
			o.external_id,
			o.name,
			null::text,
			null::text,
			coalesce(o.settings, '{}'::jsonb),
			o.created_at,
			o.updated_at,
			u.role,
			u.added_at
		from users u
		join organizations o on o.id = u.organization_id
		where u.deleted_at is null
		  and (
			u.external_id = $1
			or u.uuid::text = $1
			or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $1
		  )
		order by
			case when o.uuid::text = $2 or o.external_id = $2 then 0 else 1 end,
			u.added_at asc,
			u.id asc
	`, strings.TrimSpace(userExternalID), strings.TrimSpace(preferredOrgUUID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []platform.UserOrganizationRecord{}
	for rows.Next() {
		var org platform.UserOrganizationRecord
		settingsBytes := []byte{}
		if err := rows.Scan(
			&org.UUID,
			&org.ExternalID,
			&org.Name,
			&org.Domain,
			&org.ParentOrganizationUUID,
			&settingsBytes,
			&org.CreatedAt,
			&org.UpdatedAt,
			&org.Role,
			&org.AddedAt,
		); err != nil {
			return nil, err
		}
		settings, err := decodeOrganizationSettings(settingsBytes)
		if err != nil {
			return nil, err
		}
		org.Settings = settings
		out = append(out, org)
	}
	return out, rows.Err()
}

func (d *DB) GetOrganizationProfile(ctx context.Context, orgUUID string) (platform.OrganizationProfile, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	var profileBytes []byte
	if err := d.Pool.QueryRow(ctx, `
		select coalesce(profile, '{}'::jsonb)
		from organizations
		where uuid::text = $1 or external_id = $1
		limit 1
	`, strings.TrimSpace(orgUUID)).Scan(&profileBytes); err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(profileBytes)
}

func (d *DB) UpdateOrganizationProfile(ctx context.Context, orgUUID string, profile platform.OrganizationProfile) (platform.OrganizationProfile, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		return platform.OrganizationProfile{}, err
	}
	var savedBytes []byte
	if err := d.Pool.QueryRow(ctx, `
		update organizations
		set profile = $2::jsonb,
		    updated_at = current_timestamp
		where uuid::text = $1 or external_id = $1
		returning coalesce(profile, '{}'::jsonb)
	`, strings.TrimSpace(orgUUID), string(profileBytes)).Scan(&savedBytes); err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(savedBytes)
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
