package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

func (d *DB) UpsertBuiltinSkillWithVersion(ctx context.Context, skill BuiltinSkill, version BuiltinSkillVersion) (BuiltinSkill, BuiltinSkillVersion, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	defer tx.Rollback(ctx)

	createdSkill, err := scanBuiltinSkill(tx.QueryRow(ctx, `
		insert into builtin_skills (
			external_id, display_title, latest_version, created_at, updated_at, deleted_at
		)
		values ($1, $2, $3, $4, $4, null)
		on conflict (external_id) do update set
			display_title = excluded.display_title,
			latest_version = excluded.latest_version,
			updated_at = excluded.updated_at,
			deleted_at = null
		returning id, uuid::text, external_id, display_title, latest_version, created_at, updated_at, deleted_at
	`, skill.ExternalID, skill.DisplayTitle, version.Version, skill.CreatedAt))
	if err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}

	var existingSHA string
	var existingDeletedAt *time.Time
	err = tx.QueryRow(ctx, `
		select sha256, deleted_at
		from builtin_skill_versions
		where skill_id = $1 and version = $2
	`, createdSkill.ID, version.Version).Scan(&existingSHA, &existingDeletedAt)
	if err == nil && existingSHA != version.SHA256 {
		return BuiltinSkill{}, BuiltinSkillVersion{}, ErrVersionConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}

	version.SkillID = createdSkill.ID
	version.SkillExternalID = createdSkill.ExternalID
	createdVersion, err := upsertBuiltinSkillVersion(ctx, tx, version)
	if err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	if existingDeletedAt != nil {
		if _, err := tx.Exec(ctx, `
			update builtin_skills
			set latest_version = $2,
				updated_at = $3,
				deleted_at = null
			where id = $1
		`, createdSkill.ID, createdVersion.Version, createdVersion.CreatedAt); err != nil {
			return BuiltinSkill{}, BuiltinSkillVersion{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BuiltinSkill{}, BuiltinSkillVersion{}, err
	}
	return createdSkill, createdVersion, nil
}

func upsertBuiltinSkillVersion(ctx context.Context, tx skillTx, version BuiltinSkillVersion) (BuiltinSkillVersion, error) {
	return scanBuiltinSkillVersion(tx.QueryRow(ctx, `
		insert into builtin_skill_versions (
			external_id, skill_id, skill_external_id, version, name, description,
			directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at
		)
		values (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, null
		)
		on conflict (skill_id, version) do update set
			name = excluded.name,
			description = excluded.description,
			directory = excluded.directory,
			s3_bucket = excluded.s3_bucket,
			s3_key = excluded.s3_key,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			created_at = excluded.created_at,
			deleted_at = null
		returning id, uuid::text, external_id, skill_id, skill_external_id, version,
			name, description, directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at
	`, version.ExternalID, version.SkillID, version.SkillExternalID, version.Version, version.Name, version.Description,
		version.Directory, version.S3Bucket, version.S3Key, version.SizeBytes, version.SHA256, version.CreatedAt))
}

func (d *DB) ListBuiltinSkillsPage(ctx context.Context, params ListBuiltinSkillsPageParams) ([]BuiltinSkill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	rows, err := d.Pool.Query(ctx, builtinSkillSelectSQL()+`
		where deleted_at is null
		order by created_at desc, id desc
		limit $1 offset $2
	`, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	skills, err := scanBuiltinSkillRows(rows)
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
	err := d.Pool.QueryRow(ctx, `
		select count(*)
		from builtin_skills
		where deleted_at is null
	`).Scan(&count)
	return count, err
}

func (d *DB) GetBuiltinSkill(ctx context.Context, externalID string) (BuiltinSkill, error) {
	return scanBuiltinSkill(d.Pool.QueryRow(ctx, builtinSkillSelectSQL()+`
		where external_id = $1 and deleted_at is null
	`, externalID))
}

func (d *DB) ListBuiltinSkillVersionsPage(ctx context.Context, params ListBuiltinSkillVersionsPageParams) ([]BuiltinSkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	var skillID int64
	if err := d.Pool.QueryRow(ctx, `
		select id
		from builtin_skills
		where external_id = $1 and deleted_at is null
	`, params.SkillExternalID).Scan(&skillID); errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}

	rows, err := d.Pool.Query(ctx, builtinSkillVersionSelectSQL()+`
		where skill_id = $1 and deleted_at is null
		order by created_at desc, id desc
		limit $2 offset $3
	`, skillID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	versions, err := scanBuiltinSkillVersionRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) GetBuiltinSkillVersion(ctx context.Context, skillExternalID, version string) (BuiltinSkillVersion, error) {
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
	return scanBuiltinSkillVersion(d.Pool.QueryRow(ctx, builtinSkillVersionSelectSQL()+`
		where skill_external_id = $1
			and version = $2
			and deleted_at is null
	`, skillExternalID, version))
}

func (d *DB) SoftDeleteMissingBuiltinSkills(ctx context.Context, keepExternalIDs []string, deletedAt time.Time) ([]BuiltinSkillVersion, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, builtinSkillVersionSelectSQL()+`
		where deleted_at is null
			and skill_external_id <> all($1::text[])
		order by skill_external_id, version
	`, keepExternalIDs)
	if err != nil {
		return nil, err
	}
	versions, err := scanBuiltinSkillVersionRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		update builtin_skill_versions
		set deleted_at = $2
		where deleted_at is null
			and skill_external_id <> all($1::text[])
	`, keepExternalIDs, deletedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update builtin_skills
		set deleted_at = $2,
			updated_at = $2
		where deleted_at is null
			and external_id <> all($1::text[])
	`, keepExternalIDs, deletedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return versions, nil
}

func builtinSkillSelectSQL() string {
	return `
		select id, uuid::text, external_id, display_title, latest_version, created_at, updated_at, deleted_at
		from builtin_skills
	`
}

func builtinSkillVersionSelectSQL() string {
	return `
		select id, uuid::text, external_id, skill_id, skill_external_id, version,
			name, description, directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at
		from builtin_skill_versions
	`
}

func scanBuiltinSkill(row skillScanner) (BuiltinSkill, error) {
	var skill BuiltinSkill
	err := row.Scan(&skill.ID, &skill.UUID, &skill.ExternalID, &skill.DisplayTitle, &skill.LatestVersion,
		&skill.CreatedAt, &skill.UpdatedAt, &skill.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuiltinSkill{}, ErrNotFound
	}
	return skill, err
}

func scanBuiltinSkillRows(rows skillRows) ([]BuiltinSkill, error) {
	var skills []BuiltinSkill
	for rows.Next() {
		skill, err := scanBuiltinSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

func scanBuiltinSkillVersion(row skillScanner) (BuiltinSkillVersion, error) {
	var version BuiltinSkillVersion
	err := row.Scan(&version.ID, &version.UUID, &version.ExternalID, &version.SkillID, &version.SkillExternalID,
		&version.Version, &version.Name, &version.Description, &version.Directory, &version.S3Bucket,
		&version.S3Key, &version.SizeBytes, &version.SHA256, &version.CreatedAt, &version.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuiltinSkillVersion{}, ErrNotFound
	}
	return version, err
}

func scanBuiltinSkillVersionRows(rows skillRows) ([]BuiltinSkillVersion, error) {
	var versions []BuiltinSkillVersion
	for rows.Next() {
		version, err := scanBuiltinSkillVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}
