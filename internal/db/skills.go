package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

const skillDisplayTitleUniqueIndex = "skills_workspace_display_title_active_key"

type Skill struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	DisplayTitle        *string
	LatestVersion       *string
	Source              string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

type SkillVersion struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	SkillUUID           string
	SkillExternalID     string
	Version             string
	Name                string
	Description         string
	Directory           string
	S3Bucket            string
	S3Key               string
	SizeBytes           int64
	SHA256              string
	CreatedByAPIKeyUUID string
	CreatedAt           time.Time
	DeletedAt           *time.Time
}

type ListSkillsPageParams struct {
	WorkspaceUUID string
	Limit         int
	Offset        int
}

type ListSkillVersionsPageParams struct {
	WorkspaceUUID   string
	SkillExternalID string
	Limit           int
	Offset          int
}

type SkillDisplayTitleConflictError struct {
	DisplayTitle string
}

func (e *SkillDisplayTitleConflictError) Error() string {
	return "skill display_title conflicts with an existing skill"
}

func (d *DB) CreateSkillWithVersion(ctx context.Context, skill Skill, version SkillVersion) (Skill, SkillVersion, error) {
	if skill.DisplayTitle == nil {
		return Skill{}, SkillVersion{}, ErrInvalidState
	}
	var createdSkill Skill
	var createdVersion SkillVersion
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		skillMapper := NewSkillMapper(executor)
		versionMapper := NewSkillVersionMapper(executor)
		_, found, txErr := skillMapper.FindExternalIDByDisplayTitle(ctx, skill.WorkspaceUUID, *skill.DisplayTitle)
		if txErr != nil {
			return txErr
		}
		if found {
			return &SkillDisplayTitleConflictError{DisplayTitle: *skill.DisplayTitle}
		}

		row, txErr := skillMapper.Insert(ctx, insertSkillParams{
			UUID:                skill.UUID,
			ExternalID:          skill.ExternalID,
			WorkspaceUUID:       skill.WorkspaceUUID,
			CreatedByAPIKeyUUID: nullableString(skill.CreatedByAPIKeyUUID),
			DisplayTitle:        skill.DisplayTitle,
			LatestVersion:       version.Version,
			CreatedAt:           skill.CreatedAt,
		})
		if txErr != nil {
			if isUniqueViolationOnConstraint(txErr, skillDisplayTitleUniqueIndex) {
				return &SkillDisplayTitleConflictError{DisplayTitle: *skill.DisplayTitle}
			}
			return txErr
		}
		createdSkill = row.skill()
		version.SkillUUID = createdSkill.UUID
		version.SkillExternalID = createdSkill.ExternalID
		createdVersion, txErr = insertSkillVersion(ctx, versionMapper, version)
		return txErr
	})
	return createdSkill, createdVersion, err
}

func (d *DB) CreateSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID string, version SkillVersion) (Skill, SkillVersion, error) {
	var updatedSkill Skill
	var createdVersion SkillVersion
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		skillMapper := NewSkillMapper(executor)
		versionMapper := NewSkillVersionMapper(executor)
		row, txErr := skillMapper.FindForUpdateByExternalID(ctx, workspaceUUID, skillExternalID)
		skill, txErr := skillFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}
		if skill.Source != "custom" {
			return ErrInvalidState
		}

		version.WorkspaceUUID = skill.WorkspaceUUID
		version.SkillUUID = skill.UUID
		version.SkillExternalID = skill.ExternalID
		createdVersion, txErr = insertSkillVersion(ctx, versionMapper, version)
		if txErr != nil {
			return txErr
		}
		row, txErr = skillMapper.UpdateLatestVersionByExternalID(ctx, updateSkillLatestVersionParams{
			WorkspaceUUID: workspaceUUID,
			ExternalID:    skillExternalID,
			LatestVersion: createdVersion.Version,
			UpdatedAt:     createdVersion.CreatedAt,
		})
		updatedSkill, txErr = skillFromMapperRow(row, txErr)
		return txErr
	})
	return updatedSkill, createdVersion, err
}

func (d *DB) GetSkill(ctx context.Context, workspaceUUID string, externalID string) (Skill, error) {
	mapper := NewSkillMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return skillFromMapperRow(row, err)
}

func (d *DB) ListSkillsPage(ctx context.Context, params ListSkillsPageParams) ([]Skill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	mapper := NewSkillMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, params.WorkspaceUUID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	skills := skillsFromMapperRows(rows)
	hasMore := len(skills) > params.Limit
	if hasMore {
		skills = skills[:params.Limit]
	}
	return skills, hasMore, nil
}

func (d *DB) GetSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID, version string) (SkillVersion, error) {
	mapper := NewSkillVersionMapper(d.mapperDB)
	row, err := mapper.Find(ctx, workspaceUUID, skillExternalID, version)
	return skillVersionFromMapperRow(row, err)
}

func (d *DB) GetLatestSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID string) (SkillVersion, error) {
	mapper := NewSkillVersionMapper(d.mapperDB)
	row, err := mapper.FindLatest(ctx, workspaceUUID, skillExternalID)
	return skillVersionFromMapperRow(row, err)
}

func (d *DB) ListSkillVersionsPage(ctx context.Context, params ListSkillVersionsPageParams) ([]SkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	skillMapper := NewSkillMapper(d.mapperDB)
	skillUUID, err := skillMapper.FindUUIDByExternalID(ctx, params.WorkspaceUUID, params.SkillExternalID)
	if err != nil {
		return nil, false, mapNoRows(err)
	}
	versionMapper := NewSkillVersionMapper(d.mapperDB)
	rows, err := versionMapper.ListPageBySkillUUID(ctx, params.WorkspaceUUID, skillUUID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	versions := skillVersionsFromMapperRows(rows)
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) SoftDeleteSkill(ctx context.Context, workspaceUUID string, externalID string) (Skill, []SkillVersion, error) {
	var deletedSkill Skill
	var versions []SkillVersion
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		skillMapper := NewSkillMapper(executor)
		versionMapper := NewSkillVersionMapper(executor)
		row, txErr := skillMapper.FindForUpdateByExternalID(ctx, workspaceUUID, externalID)
		skill, txErr := skillFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}

		versionRows, txErr := versionMapper.ListBySkillUUID(ctx, workspaceUUID, skill.UUID)
		if txErr != nil {
			return txErr
		}
		versions = skillVersionsFromMapperRows(versionRows)
		if txErr = versionMapper.SoftDeleteBySkillUUID(ctx, workspaceUUID, skill.UUID); txErr != nil {
			return txErr
		}
		row, txErr = skillMapper.SoftDeleteByUUID(ctx, workspaceUUID, skill.UUID)
		deletedSkill, txErr = skillFromMapperRow(row, txErr)
		return txErr
	})
	return deletedSkill, versions, err
}

func (d *DB) SoftDeleteSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID, version string) (SkillVersion, *string, error) {
	var deletedVersion SkillVersion
	var latestVersion *string
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		skillMapper := NewSkillMapper(executor)
		versionMapper := NewSkillVersionMapper(executor)
		row, txErr := skillMapper.FindForUpdateByExternalID(ctx, workspaceUUID, skillExternalID)
		skill, txErr := skillFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}

		versionRow, txErr := versionMapper.SoftDeleteByVersion(ctx, workspaceUUID, skill.UUID, version)
		deletedVersion, txErr = skillVersionFromMapperRow(versionRow, txErr)
		if txErr != nil {
			return txErr
		}
		latest, found, txErr := versionMapper.FindLatestVersion(ctx, workspaceUUID, skill.UUID)
		if txErr != nil {
			return txErr
		}
		if found {
			latestVersion = &latest
		}
		return skillMapper.UpdateLatestVersionByUUID(ctx, workspaceUUID, skill.UUID, latestVersion)
	})
	return deletedVersion, latestVersion, err
}

func insertSkillVersion(ctx context.Context, mapper SkillVersionMapper, version SkillVersion) (SkillVersion, error) {
	row, err := mapper.Insert(ctx, insertSkillVersionParams{
		UUID:                version.UUID,
		ExternalID:          version.ExternalID,
		WorkspaceUUID:       version.WorkspaceUUID,
		SkillUUID:           version.SkillUUID,
		SkillExternalID:     version.SkillExternalID,
		Version:             version.Version,
		Name:                version.Name,
		Description:         version.Description,
		Directory:           version.Directory,
		S3Bucket:            version.S3Bucket,
		S3Key:               version.S3Key,
		SizeBytes:           version.SizeBytes,
		SHA256:              version.SHA256,
		CreatedByAPIKeyUUID: nullableString(version.CreatedByAPIKeyUUID),
		CreatedAt:           version.CreatedAt,
	})
	return skillVersionFromMapperRow(row, err)
}

func skillFromMapperRow(row skillRow, err error) (Skill, error) {
	if err != nil {
		return Skill{}, mapNoRows(err)
	}
	return row.skill(), nil
}

func skillVersionFromMapperRow(row skillVersionRow, err error) (SkillVersion, error) {
	if err != nil {
		return SkillVersion{}, mapNoRows(err)
	}
	return row.version(), nil
}

func skillsFromMapperRows(rows []skillRow) []Skill {
	skills := make([]Skill, len(rows))
	for index := range rows {
		skills[index] = rows[index].skill()
	}
	return skills
}

func skillVersionsFromMapperRows(rows []skillVersionRow) []SkillVersion {
	versions := make([]SkillVersion, len(rows))
	for index := range rows {
		versions[index] = rows[index].version()
	}
	return versions
}

func (r skillRow) skill() Skill {
	return Skill{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID,
		CreatedByAPIKeyUUID: stringFromNullable(r.CreatedByAPIKeyUUID),
		DisplayTitle:        r.DisplayTitle,
		LatestVersion:       r.LatestVersion,
		Source:              r.Source,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		DeletedAt:           r.DeletedAt,
	}
}

func (r skillVersionRow) version() SkillVersion {
	return SkillVersion{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID,
		SkillUUID:           r.SkillUUID,
		SkillExternalID:     r.SkillExternalID,
		Version:             r.Version,
		Name:                r.Name,
		Description:         r.Description,
		Directory:           r.Directory,
		S3Bucket:            r.S3Bucket,
		S3Key:               r.S3Key,
		SizeBytes:           r.SizeBytes,
		SHA256:              r.SHA256,
		CreatedByAPIKeyUUID: stringFromNullable(r.CreatedByAPIKeyUUID),
		CreatedAt:           r.CreatedAt,
		DeletedAt:           r.DeletedAt,
	}
}
