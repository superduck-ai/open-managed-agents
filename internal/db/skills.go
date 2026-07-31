package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
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

type skillRow struct {
	UUID                uuid.UUID  `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       uuid.UUID  `db:"workspace_uuid"`
	CreatedByAPIKeyUUID uuid.UUID  `db:"created_by_api_key_uuid"`
	DisplayTitle        *string    `db:"display_title"`
	LatestVersion       *string    `db:"latest_version"`
	Source              string     `db:"source"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type skillVersionRow struct {
	UUID                uuid.UUID  `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       uuid.UUID  `db:"workspace_uuid"`
	SkillUUID           uuid.UUID  `db:"skill_uuid"`
	SkillExternalID     string     `db:"skill_external_id"`
	Version             string     `db:"version"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Directory           string     `db:"directory"`
	S3Bucket            string     `db:"s3_bucket"`
	S3Key               string     `db:"s3_key"`
	SizeBytes           int64      `db:"size_bytes"`
	SHA256              string     `db:"sha256"`
	CreatedByAPIKeyUUID uuid.UUID  `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time  `db:"created_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	defer tx.Rollback()

	if skill.DisplayTitle == nil {
		return Skill{}, SkillVersion{}, ErrInvalidState
	}

	var existingID string
	err = namedGetContext(ctx, tx, &existingID, `
		select external_id
		from skills
		where workspace_uuid = :workspace_uuid
			and display_title = :display_title
			and deleted_at is null
			limit 1
	`, map[string]any{
		"workspace_uuid": dbUUID(skill.WorkspaceUUID),
		"display_title":  *skill.DisplayTitle,
	})
	if err == nil {
		return Skill{}, SkillVersion{}, &SkillDisplayTitleConflictError{DisplayTitle: *skill.DisplayTitle}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Skill{}, SkillVersion{}, err
	}

	createdSkill, err := getSkillSQLX(ctx, tx, `
		insert into skills (
			uuid, external_id, workspace_uuid, created_by_api_key_uuid,
			display_title, latest_version, source, created_at, updated_at
		)
		values (
			:uuid, :external_id, :workspace_uuid, :created_by_api_key_uuid,
			:display_title, :latest_version, 'custom', :created_at, :created_at
		)
		returning `+skillColumns()+`
	`, map[string]any{
		"uuid":                    dbUUID(skill.UUID),
		"external_id":             skill.ExternalID,
		"workspace_uuid":          dbUUID(skill.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(skill.CreatedByAPIKeyUUID),
		"display_title":           skill.DisplayTitle,
		"latest_version":          version.Version,
		"created_at":              skill.CreatedAt,
	})
	if err != nil {
		if isUniqueViolationOnConstraint(err, skillDisplayTitleUniqueIndex) {
			return Skill{}, SkillVersion{}, &SkillDisplayTitleConflictError{DisplayTitle: *skill.DisplayTitle}
		}
		return Skill{}, SkillVersion{}, err
	}

	version.SkillUUID = createdSkill.UUID
	version.SkillExternalID = createdSkill.ExternalID
	createdVersion, err := insertSkillVersion(ctx, tx, version)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, SkillVersion{}, err
	}
	return createdSkill, createdVersion, nil
}

func (d *DB) CreateSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID string, version SkillVersion) (Skill, SkillVersion, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	defer tx.Rollback()

	skill, err := getSkillSQLX(ctx, tx, skillSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    skillExternalID,
	})
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if skill.Source != "custom" {
		return Skill{}, SkillVersion{}, ErrInvalidState
	}

	version.WorkspaceUUID = skill.WorkspaceUUID
	version.SkillUUID = skill.UUID
	version.SkillExternalID = skill.ExternalID
	createdVersion, err := insertSkillVersion(ctx, tx, version)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	updatedSkill, err := getSkillSQLX(ctx, tx, `
		update skills
		set latest_version = :latest_version,
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+skillColumns()+`
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    skillExternalID,
		"latest_version": createdVersion.Version,
		"updated_at":     createdVersion.CreatedAt,
	})
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, SkillVersion{}, err
	}
	return updatedSkill, createdVersion, nil
}

func (d *DB) GetSkill(ctx context.Context, workspaceUUID string, externalID string) (Skill, error) {
	return getSkillSQLX(ctx, d.sql, skillSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
	})
}

func (d *DB) ListSkillsPage(ctx context.Context, params ListSkillsPageParams) ([]Skill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	skills, err := selectSkillsSQLX(ctx, d.sql, skillSelectSQL()+`
		where workspace_uuid = :workspace_uuid and deleted_at is null
		order by created_at desc, uuid desc
		limit :limit offset :offset
	`, map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
		"offset":         params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	hasMore := len(skills) > params.Limit
	if hasMore {
		skills = skills[:params.Limit]
	}
	return skills, hasMore, nil
}

func (d *DB) GetSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID, version string) (SkillVersion, error) {
	return getSkillVersionSQLX(ctx, d.sql, skillVersionSelectSQL()+`
			where workspace_uuid = :workspace_uuid
				and skill_external_id = :skill_external_id
				and version = :version
				and deleted_at is null
		`, map[string]any{
		"workspace_uuid":    dbUUID(workspaceUUID),
		"skill_external_id": skillExternalID,
		"version":           version,
	})
}

func (d *DB) GetLatestSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID string) (SkillVersion, error) {
	return getSkillVersionSQLX(ctx, d.sql, `
			select sv.uuid, sv.external_id,
				sv.workspace_uuid, sv.skill_uuid,
				sv.skill_external_id,
				sv.version, sv.name, sv.description, sv.directory, sv.s3_bucket, sv.s3_key, sv.size_bytes, sv.sha256,
				sv.created_by_api_key_uuid, sv.created_at, sv.deleted_at
			from skills s
			join skill_versions sv
				on sv.skill_uuid = s.uuid
				and sv.version = s.latest_version
				and sv.deleted_at is null
			where s.workspace_uuid = :workspace_uuid
				and s.external_id = :skill_external_id
				and s.deleted_at is null
				and s.latest_version is not null
				and s.latest_version <> ''
		`, map[string]any{
		"workspace_uuid":    dbUUID(workspaceUUID),
		"skill_external_id": skillExternalID,
	})
}

func (d *DB) ListSkillVersionsPage(ctx context.Context, params ListSkillVersionsPageParams) ([]SkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	var skillUUID uuid.UUID
	if err := namedGetContext(ctx, d.sql, &skillUUID, `
		select uuid
		from skills
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"external_id":    params.SkillExternalID,
	}); errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}

	versions, err := selectSkillVersionsSQLX(ctx, d.sql, skillVersionSelectSQL()+`
		where skill_uuid = :skill_uuid and deleted_at is null
		order by created_at desc, uuid desc
		limit :limit offset :offset
	`, map[string]any{"skill_uuid": skillUUID, "limit": params.Limit + 1, "offset": params.Offset})
	if err != nil {
		return nil, false, err
	}
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) SoftDeleteSkill(ctx context.Context, workspaceUUID string, externalID string) (Skill, []SkillVersion, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Skill{}, nil, err
	}
	defer tx.Rollback()

	skill, err := getSkillSQLX(ctx, tx, skillSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
	})
	if err != nil {
		return Skill{}, nil, err
	}

	versions, err := selectSkillVersionsSQLX(ctx, tx, skillVersionSelectSQL()+`
		where skill_uuid = :skill_uuid and deleted_at is null
		order by created_at desc, uuid desc
	`, map[string]any{"skill_uuid": dbUUID(skill.UUID)})
	if err != nil {
		return Skill{}, nil, err
	}

	if _, err := namedExecContext(ctx, tx, `
		update skill_versions
		set deleted_at = now()
		where skill_uuid = :skill_uuid and deleted_at is null
	`, map[string]any{"skill_uuid": dbUUID(skill.UUID)}); err != nil {
		return Skill{}, nil, err
	}
	deletedSkill, err := getSkillSQLX(ctx, tx, `
		update skills
		set deleted_at = now(),
			updated_at = now()
		where uuid = :skill_uuid and deleted_at is null
		returning `+skillColumns()+`
	`, map[string]any{"skill_uuid": dbUUID(skill.UUID)})
	if err != nil {
		return Skill{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, nil, err
	}
	return deletedSkill, versions, nil
}

func (d *DB) SoftDeleteSkillVersion(ctx context.Context, workspaceUUID string, skillExternalID, version string) (SkillVersion, *string, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return SkillVersion{}, nil, err
	}
	defer tx.Rollback()

	skill, err := getSkillSQLX(ctx, tx, skillSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    skillExternalID,
	})
	if err != nil {
		return SkillVersion{}, nil, err
	}

	deletedVersion, err := getSkillVersionSQLX(ctx, tx, `
		update skill_versions
		set deleted_at = now()
		where skill_uuid = :skill_uuid and version = :version and deleted_at is null
		returning `+skillVersionColumns()+`
	`, map[string]any{"skill_uuid": dbUUID(skill.UUID), "version": version})
	if err != nil {
		return SkillVersion{}, nil, err
	}

	var latestVersion *string
	var latest string
	err = namedGetContext(ctx, tx, &latest, `
		select version
		from skill_versions
		where skill_uuid = :skill_uuid and deleted_at is null
		order by created_at desc, uuid desc
		limit 1
	`, map[string]any{"skill_uuid": dbUUID(skill.UUID)})
	if errors.Is(err, sql.ErrNoRows) {
		latestVersion = nil
	} else if err != nil {
		return SkillVersion{}, nil, err
	} else {
		latestVersion = &latest
	}

	if _, err := namedExecContext(ctx, tx, `
		update skills
		set latest_version = :latest_version,
			updated_at = now()
		where uuid = :skill_uuid
	`, map[string]any{
		"skill_uuid":     dbUUID(skill.UUID),
		"latest_version": latestVersion,
	}); err != nil {
		return SkillVersion{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return SkillVersion{}, nil, err
	}
	return deletedVersion, latestVersion, nil
}

const insertSkillVersionQuery = `
		insert into skill_versions (
			uuid, external_id, workspace_uuid, skill_uuid, skill_external_id, version,
			name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_uuid, created_at
		)
		values (
			:uuid, :external_id, :workspace_uuid, :skill_uuid, :skill_external_id, :version,
			:name, :description, :directory, :s3_bucket, :s3_key, :size_bytes, :sha256,
			:created_by_api_key_uuid, :created_at
		)
		returning ` + `uuid, external_id,
			workspace_uuid, skill_uuid, skill_external_id,
			version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_uuid, created_at, deleted_at`

func insertSkillVersion(ctx context.Context, database sqlxNamedQueryer, version SkillVersion) (SkillVersion, error) {
	return getSkillVersionSQLX(ctx, database, insertSkillVersionQuery, map[string]any{
		"uuid":                    dbUUID(version.UUID),
		"external_id":             version.ExternalID,
		"workspace_uuid":          dbUUID(version.WorkspaceUUID),
		"skill_uuid":              dbUUID(version.SkillUUID),
		"skill_external_id":       version.SkillExternalID,
		"version":                 version.Version,
		"name":                    version.Name,
		"description":             version.Description,
		"directory":               version.Directory,
		"s3_bucket":               version.S3Bucket,
		"s3_key":                  version.S3Key,
		"size_bytes":              version.SizeBytes,
		"sha256":                  version.SHA256,
		"created_by_api_key_uuid": dbUUID(version.CreatedByAPIKeyUUID),
		"created_at":              version.CreatedAt,
	})
}

func skillSelectSQL() string {
	return `select ` + skillColumns() + ` from skills`
}

func skillVersionSelectSQL() string {
	return `select ` + skillVersionColumns() + ` from skill_versions`
}

func skillColumns() string {
	return `uuid, external_id, workspace_uuid,
		created_by_api_key_uuid,
		display_title, latest_version, source, created_at, updated_at, deleted_at`
}

func skillVersionColumns() string {
	return `uuid, external_id, workspace_uuid,
		skill_uuid, skill_external_id,
		version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
		created_by_api_key_uuid, created_at, deleted_at`
}

func getSkillSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (Skill, error) {
	var row skillRow
	if err := namedGetContext(ctx, database, &row, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return Skill{}, ErrNotFound
	} else if err != nil {
		return Skill{}, err
	}
	return row.skill(), nil
}

func selectSkillsSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]Skill, error) {
	var rows []skillRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	skills := make([]Skill, len(rows))
	for index := range rows {
		skills[index] = rows[index].skill()
	}
	return skills, nil
}

func getSkillVersionSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (SkillVersion, error) {
	var row skillVersionRow
	if err := namedGetContext(ctx, database, &row, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return SkillVersion{}, ErrNotFound
	} else if err != nil {
		return SkillVersion{}, err
	}
	return row.version(), nil
}

func selectSkillVersionsSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]SkillVersion, error) {
	var rows []skillVersionRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	versions := make([]SkillVersion, len(rows))
	for index := range rows {
		versions[index] = rows[index].version()
	}
	return versions, nil
}

func (r skillRow) skill() Skill {
	return Skill{
		UUID:                r.UUID.String(),
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID.String(),
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID.String(),
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
		UUID:                r.UUID.String(),
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID.String(),
		SkillUUID:           r.SkillUUID.String(),
		SkillExternalID:     r.SkillExternalID,
		Version:             r.Version,
		Name:                r.Name,
		Description:         r.Description,
		Directory:           r.Directory,
		S3Bucket:            r.S3Bucket,
		S3Key:               r.S3Key,
		SizeBytes:           r.SizeBytes,
		SHA256:              r.SHA256,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID.String(),
		CreatedAt:           r.CreatedAt,
		DeletedAt:           r.DeletedAt,
	}
}
