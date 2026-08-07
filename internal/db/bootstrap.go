package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func (d *DB) FindBootstrapUserContext(ctx context.Context, preferredOrgUUID string) (string, string, error) {
	if d == nil || d.mapperDB == nil {
		return "", "", platform.ErrNotFound
	}
	mapper := NewConsoleUserMapper(d.mapperDB)
	row, err := mapper.FindBootstrapContext(ctx, strings.TrimSpace(preferredOrgUUID))
	if err != nil {
		return "", "", mapNoRows(err)
	}
	return row.UserExternalID, row.OrgUUID, nil
}

func (d *DB) GetBootstrapUser(ctx context.Context, userExternalID string) (*platform.UserRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(userExternalID) == "" {
		return nil, platform.ErrNotFound
	}
	userExternalID = strings.TrimSpace(userExternalID)
	mapper := NewConsoleUserMapper(d.mapperDB)
	row, err := mapper.FindBootstrapUser(ctx, userExternalID, tryParseDBUUIDIdentifierString(userExternalID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &platform.UserRecord{
		UUID:          row.UUID,
		ExternalID:    row.ExternalID,
		Email:         row.Email,
		FullName:      row.FullName,
		DisplayName:   row.DisplayName,
		IsVerified:    row.IsVerified,
		AgeIsVerified: row.AgeIsVerified,
		Settings:      map[string]any{},
		CreatedAt:     row.CreatedAt,
	}, nil
}

func (d *DB) GetPlatformOrganization(ctx context.Context, orgUUID string) (*platform.OrganizationRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, platform.ErrNotFound
	}
	mapper := NewConsoleOrganizationMapper(d.mapperDB)
	row, err := mapper.FindByUUID(ctx, strings.TrimSpace(orgUUID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return row.organizationRecord()
}

func (d *DB) UpdatePlatformOrganization(ctx context.Context, orgUUID string, patch platform.OrganizationUpdatePatch) (*platform.OrganizationRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, platform.ErrNotFound
	}
	current, err := d.GetPlatformOrganization(ctx, orgUUID)
	if err != nil {
		return nil, err
	}
	var name *string
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		name = &trimmed
	}
	var settingsBytes []byte
	if patch.Settings != nil {
		settings := cloneOrganizationSettings(current.Settings)
		mergeOrganizationSettings(settings, patch.Settings)
		settingsBytes, err = json.Marshal(settings)
		if err != nil {
			return nil, err
		}
	}
	mapper := NewConsoleOrganizationMapper(d.mapperDB)
	row, err := mapper.UpdateByUUID(ctx, updateConsoleOrganizationParams{
		OrgUUID:  strings.TrimSpace(orgUUID),
		Name:     name,
		Settings: settingsBytes,
	})
	if err != nil {
		return nil, mapNoRows(err)
	}
	return row.organizationRecord()
}

func (d *DB) ListBootstrapUserOrganizations(ctx context.Context, userExternalID string, preferredOrgUUID string) ([]platform.UserOrganizationRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(userExternalID) == "" {
		return []platform.UserOrganizationRecord{}, nil
	}
	userExternalID = strings.TrimSpace(userExternalID)
	mapper := NewConsoleUserMapper(d.mapperDB)
	rows, err := mapper.ListBootstrapOrganizations(
		ctx,
		userExternalID,
		tryParseDBUUIDIdentifierString(userExternalID),
		tryParseDBUUIDIdentifierString(preferredOrgUUID),
	)
	if err != nil {
		return nil, err
	}
	out := make([]platform.UserOrganizationRecord, 0, len(rows))
	for _, row := range rows {
		org, mapErr := row.userOrganizationRecord()
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, org)
	}
	return out, nil
}

func (d *DB) GetOrganizationProfile(ctx context.Context, orgUUID string) (platform.OrganizationProfile, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	mapper := NewConsoleOrganizationMapper(d.mapperDB)
	row, err := mapper.FindProfileByUUID(ctx, strings.TrimSpace(orgUUID))
	if err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(row.Profile)
}

func (d *DB) UpdateOrganizationProfile(ctx context.Context, orgUUID string, profile platform.OrganizationProfile) (platform.OrganizationProfile, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return platform.OrganizationProfile{}, platform.ErrNotFound
	}
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		return platform.OrganizationProfile{}, err
	}
	mapper := NewConsoleOrganizationMapper(d.mapperDB)
	row, err := mapper.UpdateProfileByUUID(ctx, updateConsoleOrganizationProfileParams{
		OrgUUID: strings.TrimSpace(orgUUID),
		Profile: profileBytes,
	})
	if err != nil {
		return platform.OrganizationProfile{}, mapNoRows(err)
	}
	return decodeOrganizationProfile(row.Profile)
}

func (row consoleOrganizationRow) organizationRecord() (*platform.OrganizationRecord, error) {
	settings, err := decodeOrganizationSettings(row.Settings)
	if err != nil {
		return nil, err
	}
	var parentOrganizationUUID *string
	if row.ParentOrganizationUUID.Valid {
		value := row.ParentOrganizationUUID.String
		parentOrganizationUUID = &value
	}
	return &platform.OrganizationRecord{
		UUID:                   row.UUID,
		Name:                   row.Name,
		Domain:                 row.Domain,
		ParentOrganizationUUID: parentOrganizationUUID,
		Settings:               settings,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}, nil
}

func (row consoleOrganizationRow) userOrganizationRecord() (platform.UserOrganizationRecord, error) {
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
