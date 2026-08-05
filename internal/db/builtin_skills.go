package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type BuiltinSkill struct {
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
	SkillID         int64
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
	SkillExternalID string
	Limit           int
	Offset          int
}

func (d *DB) UpsertBuiltinSkillWithVersion(
	ctx context.Context,
	skill BuiltinSkill,
	version BuiltinSkillVersion,
) (BuiltinSkill, BuiltinSkillVersion, error) {
	var createdSkill BuiltinSkill
	var createdVersion BuiltinSkillVersion
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewBuiltinSkillMapper(executor)
		skillRow, upsertErr := mapper.UpsertSkill(ctx, upsertBuiltinSkillParams{
			ExternalID:    skill.ExternalID,
			DisplayTitle:  skill.DisplayTitle,
			LatestVersion: version.Version,
			CreatedAt:     skill.CreatedAt,
		})
		if upsertErr != nil {
			return upsertErr
		}
		createdSkill = skillRow.skill()

		versionRow, upsertErr := mapper.UpsertVersion(ctx, upsertBuiltinSkillVersionParams{
			ExternalID:      version.ExternalID,
			SkillID:         createdSkill.ID,
			SkillExternalID: createdSkill.ExternalID,
			Version:         version.Version,
			Name:            version.Name,
			Description:     version.Description,
			Directory:       version.Directory,
			S3Bucket:        version.S3Bucket,
			S3Key:           version.S3Key,
			SizeBytes:       version.SizeBytes,
			SHA256:          version.SHA256,
			CreatedAt:       version.CreatedAt,
		})
		if errors.Is(mapNoRows(upsertErr), ErrNotFound) {
			return ErrVersionConflict
		}
		if upsertErr != nil {
			return upsertErr
		}
		createdVersion = versionRow.skillVersion()
		return nil
	})
	return createdSkill, createdVersion, err
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
	skillID, err := mapper.FindSkillIDByExternalID(ctx, params.SkillExternalID)
	if err != nil {
		return nil, false, mapNoRows(err)
	}
	rows, err := mapper.ListVersionsPage(ctx, skillID, params.Limit+1, params.Offset)
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

func (d *DB) SoftDeleteMissingBuiltinSkills(
	ctx context.Context,
	keepExternalIDs []string,
	deletedAt time.Time,
) ([]BuiltinSkillVersion, error) {
	keepExternalIDsJSON, err := json.Marshal(append([]string{}, keepExternalIDs...))
	if err != nil {
		return nil, err
	}
	params := pruneBuiltinSkillsParams{
		KeepExternalIDsJSON: keepExternalIDsJSON,
		DeletedAt:           deletedAt,
	}
	var versions []BuiltinSkillVersion
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewBuiltinSkillMapper(executor)
		rows, listErr := mapper.ListMissingVersions(ctx, keepExternalIDsJSON)
		if listErr != nil {
			return listErr
		}
		versions = builtinSkillVersionsFromRows(rows)
		if deleteErr := mapper.SoftDeleteMissingVersions(ctx, params); deleteErr != nil {
			return deleteErr
		}
		return mapper.SoftDeleteMissingSkills(ctx, params)
	})
	return versions, err
}

func (r builtinSkillRow) skill() BuiltinSkill {
	return BuiltinSkill{
		ID:            r.ID,
		UUID:          r.UUID,
		ExternalID:    r.ExternalID,
		DisplayTitle:  r.DisplayTitle,
		LatestVersion: r.LatestVersion,
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
		SkillID:         r.SkillID,
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
