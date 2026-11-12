package db

import (
	"context"
	"strings"
	"time"
)

type BuiltinSkill struct {
	IconKey       string
	Listed        bool
	ID            int64
	UUID          string
	ExternalID    string
	DisplayTitle  string
	LatestVersion *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

type BuiltinSkillVersion struct {
	ID              int64
	UUID            string
	ExternalID      string
	SkillUUID       string
	SkillExternalID string
	Version         string
	Name            string
	Description     string
	Directory       string
	S3Bucket        string
	S3Key           string
	SizeBytes       int64
	SHA256          string
	CreatedAt       time.Time
	DeletedAt       *time.Time
}

type ListBuiltinSkillsPageParams struct {
	Limit  int
	Offset int
}

type ListBuiltinSkillVersionsPageParams struct {
	OldestFirst     bool
	SkillExternalID string
	Limit           int
	Offset          int
}

func (d *DB) ListBuiltinSkillsPage(
	ctx context.Context,
	params ListBuiltinSkillsPageParams,
) ([]BuiltinSkill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	mapper := NewBuiltinSkillMapper(d.mapperDB)
	rows, err := mapper.ListSkillsPage(ctx, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	skills := builtinSkillsFromRows(rows)
	hasMore := len(skills) > params.Limit
	if hasMore {
		skills = skills[:params.Limit]
	}
	return skills, hasMore, nil
}

func (d *DB) CountBuiltinSkills(ctx context.Context) (int, error) {
	mapper := NewBuiltinSkillMapper(d.mapperDB)
	return mapper.CountSkills(ctx)
}

func (d *DB) GetBuiltinSkill(ctx context.Context, externalID string) (BuiltinSkill, error) {
	mapper := NewBuiltinSkillMapper(d.mapperDB)
	row, err := mapper.FindSkillByExternalID(ctx, externalID)
	return builtinSkillFromRow(row, err)
}

func (d *DB) ListBuiltinSkillVersionsPage(
	ctx context.Context,
	params ListBuiltinSkillVersionsPageParams,
) ([]BuiltinSkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	mapper := NewBuiltinSkillMapper(d.mapperDB)
	skillUUID, err := mapper.FindSkillUUIDByExternalID(ctx, params.SkillExternalID)
	if err != nil {
		return nil, false, mapNoRows(err)
	}
	rows, err := mapper.ListVersionsPage(ctx, skillUUID, params.Limit+1, params.Offset, params.OldestFirst)
	if err != nil {
		return nil, false, err
	}
	versions := builtinSkillVersionsFromRows(rows)
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) GetBuiltinSkillVersion(
	ctx context.Context,
	skillExternalID string,
	version string,
) (BuiltinSkillVersion, error) {
	if version == "latest" {
		skill, err := d.GetBuiltinSkill(ctx, skillExternalID)
		if err != nil {
			return BuiltinSkillVersion{}, err
		}
		if skill.LatestVersion == nil || strings.TrimSpace(*skill.LatestVersion) == "" {
			return BuiltinSkillVersion{}, ErrNotFound
		}
		version = *skill.LatestVersion
	}
	mapper := NewBuiltinSkillMapper(d.mapperDB)
	row, err := mapper.FindVersion(ctx, skillExternalID, version)
	return builtinSkillVersionFromRow(row, err)
}

func (r builtinSkillRow) skill() BuiltinSkill {
	return BuiltinSkill{
		ID:            r.ID,
		UUID:          r.UUID,
		ExternalID:    r.ExternalID,
		DisplayTitle:  r.DisplayTitle,
		LatestVersion: r.LatestVersion,
		IconKey:       r.IconKey,
		Listed:        r.Listed,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		DeletedAt:     r.DeletedAt,
	}
}

func (r builtinSkillVersionRow) skillVersion() BuiltinSkillVersion {
	return BuiltinSkillVersion{
		ID:              r.ID,
		UUID:            r.UUID,
		ExternalID:      r.ExternalID,
		SkillUUID:       r.SkillUUID,
		SkillExternalID: r.SkillExternalID,
		Version:         r.Version,
		Name:            r.Name,
		Description:     r.Description,
		Directory:       r.Directory,
		S3Bucket:        r.S3Bucket,
		S3Key:           r.S3Key,
		SizeBytes:       r.SizeBytes,
		SHA256:          r.SHA256,
		CreatedAt:       r.CreatedAt,
		DeletedAt:       r.DeletedAt,
	}
}

func builtinSkillFromRow(row builtinSkillRow, err error) (BuiltinSkill, error) {
	if err != nil {
		return BuiltinSkill{}, mapNoRows(err)
	}
	return row.skill(), nil
}

func builtinSkillVersionFromRow(row builtinSkillVersionRow, err error) (BuiltinSkillVersion, error) {
	if err != nil {
		return BuiltinSkillVersion{}, mapNoRows(err)
	}
	return row.skillVersion(), nil
}

func builtinSkillsFromRows(rows []builtinSkillRow) []BuiltinSkill {
	skills := make([]BuiltinSkill, len(rows))
	for index := range rows {
		skills[index] = rows[index].skill()
	}
	return skills
}

func builtinSkillVersionsFromRows(rows []builtinSkillVersionRow) []BuiltinSkillVersion {
	versions := make([]BuiltinSkillVersion, len(rows))
	for index := range rows {
		versions[index] = rows[index].skillVersion()
	}
	return versions
}
