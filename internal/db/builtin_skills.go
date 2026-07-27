package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	builtinSkillColumns = `
		id,
		CAST(uuid AS text) AS uuid,
		external_id,
		display_title,
		latest_version,
		created_at,
		updated_at,
		deleted_at
	`
	builtinSkillVersionColumns = `
		id,
		CAST(uuid AS text) AS uuid,
		external_id,
		skill_id,
		skill_external_id,
		version,
		name,
		description,
		directory,
		s3_bucket,
		s3_key,
		size_bytes,
		sha256,
		created_at,
		deleted_at
	`
	upsertBuiltinSkillQuery = `
		insert into builtin_skills (
			external_id, display_title, latest_version, created_at, updated_at, deleted_at
		)
		values (:external_id, :display_title, :latest_version, :created_at, :created_at, null)
		on conflict (external_id) do update set
			display_title = excluded.display_title,
			latest_version = excluded.latest_version,
			updated_at = excluded.updated_at,
			deleted_at = null
		returning ` + builtinSkillColumns + `
	`
	upsertBuiltinSkillVersionQuery = `
		insert into builtin_skill_versions (
			external_id, skill_id, skill_external_id, version, name, description,
			directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at
		)
		values (
			:external_id, :skill_id, :skill_external_id, :version, :name, :description,
			:directory, :s3_bucket, :s3_key, :size_bytes, :sha256, :created_at, null
		)
		on conflict (skill_id, version) do update set
			name = excluded.name,
			description = excluded.description,
			directory = excluded.directory,
			s3_bucket = excluded.s3_bucket,
			s3_key = excluded.s3_key,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			created_at = case
				when builtin_skill_versions.deleted_at is not null then excluded.created_at
				else builtin_skill_versions.created_at
			end,
			deleted_at = null
		where builtin_skill_versions.sha256 = excluded.sha256
		returning ` + builtinSkillVersionColumns + `
	`
	listBuiltinSkillsPageQuery = `
		select ` + builtinSkillColumns + `
		from builtin_skills
		where deleted_at is null
		order by created_at desc, id desc
		limit :limit offset :offset
	`
	countBuiltinSkillsQuery = `
		select count(*)
		from builtin_skills
		where deleted_at is null
	`
	getBuiltinSkillQuery = `
		select ` + builtinSkillColumns + `
		from builtin_skills
		where external_id = :external_id and deleted_at is null
	`
	getBuiltinSkillIDQuery = `
		select id
		from builtin_skills
		where external_id = :external_id and deleted_at is null
	`
	listBuiltinSkillVersionsPageQuery = `
		select ` + builtinSkillVersionColumns + `
		from builtin_skill_versions
		where skill_id = :skill_id and deleted_at is null
		order by created_at desc, id desc
		limit :limit offset :offset
	`
	getBuiltinSkillVersionQuery = `
		select ` + builtinSkillVersionColumns + `
		from builtin_skill_versions
		where skill_external_id = :skill_external_id
			and version = :version
			and deleted_at is null
	`
	listMissingBuiltinSkillVersionsQuery = `
		select ` + builtinSkillVersionColumns + `
		from builtin_skill_versions
		where deleted_at is null
			and not exists (
				select 1
				from jsonb_array_elements_text(CAST(:keep_external_ids AS jsonb)) AS kept(external_id)
				where kept.external_id = builtin_skill_versions.skill_external_id
			)
		order by skill_external_id, version
	`
	softDeleteMissingBuiltinSkillVersionsQuery = `
		update builtin_skill_versions
		set deleted_at = :deleted_at
		where deleted_at is null
			and not exists (
				select 1
				from jsonb_array_elements_text(CAST(:keep_external_ids AS jsonb)) AS kept(external_id)
				where kept.external_id = builtin_skill_versions.skill_external_id
			)
	`
	softDeleteMissingBuiltinSkillsQuery = `
		update builtin_skills
		set deleted_at = :deleted_at,
			updated_at = :deleted_at
		where deleted_at is null
			and not exists (
				select 1
				from jsonb_array_elements_text(CAST(:keep_external_ids AS jsonb)) AS kept(external_id)
				where kept.external_id = builtin_skills.external_id
			)
	`
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

type builtinSkillRow struct {
	ID            int64      `db:"id"`
	UUID          string     `db:"uuid"`
	ExternalID    string     `db:"external_id"`
	DisplayTitle  string     `db:"display_title"`
	LatestVersion *string    `db:"latest_version"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	DeletedAt     *time.Time `db:"deleted_at"`
}

type builtinSkillVersionRow struct {
	ID              int64      `db:"id"`
	UUID            string     `db:"uuid"`
	ExternalID      string     `db:"external_id"`
	SkillID         int64      `db:"skill_id"`
	SkillExternalID string     `db:"skill_external_id"`
	Version         string     `db:"version"`
	Name            string     `db:"name"`
	Description     string     `db:"description"`
	Directory       string     `db:"directory"`
	S3Bucket        string     `db:"s3_bucket"`
	S3Key           string     `db:"s3_key"`
	SizeBytes       int64      `db:"size_bytes"`
	SHA256          string     `db:"sha256"`
	CreatedAt       time.Time  `db:"created_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
}

func (d *DB) UpsertBuiltinSkillWithVersion(
	ctx context.Context,
	skill BuiltinSkill,
	version BuiltinSkillVersion,
) (BuiltinSkill, BuiltinSkillVersion, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	defer func() { _ = tx.Rollback() }()

	createdSkill, err := getBuiltinSkillSQLX(ctx, tx, upsertBuiltinSkillQuery, map[string]any{
		"external_id":    skill.ExternalID,
		"display_title":  skill.DisplayTitle,
		"latest_version": version.Version,
		"created_at":     skill.CreatedAt,
	})
	if err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}

	version.SkillID = createdSkill.ID
	version.SkillExternalID = createdSkill.ExternalID
	createdVersion, err := upsertBuiltinSkillVersion(ctx, tx, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return BuiltinSkill{}, BuiltinSkillVersion{}, ErrVersionConflict
		}
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	return createdSkill, createdVersion, nil
}

func upsertBuiltinSkillVersion(
	ctx context.Context,
	database sqlxNamedQueryer,
	version BuiltinSkillVersion,
) (BuiltinSkillVersion, error) {
	return getBuiltinSkillVersionSQLX(ctx, database, upsertBuiltinSkillVersionQuery, map[string]any{
		"external_id":       version.ExternalID,
		"skill_id":          version.SkillID,
		"skill_external_id": version.SkillExternalID,
		"version":           version.Version,
		"name":              version.Name,
		"description":       version.Description,
		"directory":         version.Directory,
		"s3_bucket":         version.S3Bucket,
		"s3_key":            version.S3Key,
		"size_bytes":        version.SizeBytes,
		"sha256":            version.SHA256,
		"created_at":        version.CreatedAt,
	})
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
	skills, err := selectBuiltinSkillsSQLX(ctx, d.sql, listBuiltinSkillsPageQuery, map[string]any{
		"limit":  params.Limit + 1,
		"offset": params.Offset,
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

func (d *DB) CountBuiltinSkills(ctx context.Context) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, countBuiltinSkillsQuery, map[string]any{})
	return count, err
}

func (d *DB) GetBuiltinSkill(ctx context.Context, externalID string) (BuiltinSkill, error) {
	return getBuiltinSkillSQLX(ctx, d.sql, getBuiltinSkillQuery, map[string]any{
		"external_id": externalID,
	})
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
	var skillID int64
	if err := namedGetContext(ctx, d.sql, &skillID, getBuiltinSkillIDQuery, map[string]any{
		"external_id": params.SkillExternalID,
	}); err != nil {
		return nil, false, mapNoRows(err)
	}
	versions, err := selectBuiltinSkillVersionsSQLX(
		ctx,
		d.sql,
		listBuiltinSkillVersionsPageQuery,
		map[string]any{
			"skill_id": skillID,
			"limit":    params.Limit + 1,
			"offset":   params.Offset,
		},
	)
	if err != nil {
		return nil, false, err
	}
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
	return getBuiltinSkillVersionSQLX(ctx, d.sql, getBuiltinSkillVersionQuery, map[string]any{
		"skill_external_id": skillExternalID,
		"version":           version,
	})
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
	arguments := map[string]any{
		"keep_external_ids": keepExternalIDsJSON,
		"deleted_at":        deletedAt,
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	versions, err := selectBuiltinSkillVersionsSQLX(
		ctx,
		tx,
		listMissingBuiltinSkillVersionsQuery,
		arguments,
	)
	if err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, softDeleteMissingBuiltinSkillVersionsQuery, arguments); err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, softDeleteMissingBuiltinSkillsQuery, arguments); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return versions, nil
}

func getBuiltinSkillSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (BuiltinSkill, error) {
	var row builtinSkillRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return BuiltinSkill{}, mapNoRows(err)
	}
	return row.skill(), nil
}

func selectBuiltinSkillsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]BuiltinSkill, error) {
	var rows []builtinSkillRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	skills := make([]BuiltinSkill, 0, len(rows))
	for _, row := range rows {
		skills = append(skills, row.skill())
	}
	return skills, nil
}

func getBuiltinSkillVersionSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (BuiltinSkillVersion, error) {
	var row builtinSkillVersionRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return BuiltinSkillVersion{}, mapNoRows(err)
	}
	return row.skillVersion(), nil
}

func selectBuiltinSkillVersionsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]BuiltinSkillVersion, error) {
	var rows []builtinSkillVersionRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	versions := make([]BuiltinSkillVersion, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, row.skillVersion())
	}
	return versions, nil
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
