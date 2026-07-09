package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Skill struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	CreatedByAPIKeyID int64
	DisplayTitle      *string
	LatestVersion     *string
	Source            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type SkillVersion struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	SkillID           int64
	SkillExternalID   string
	Version           string
	Name              string
	Description       string
	Directory         string
	S3Bucket          string
	S3Key             string
	SizeBytes         int64
	SHA256            string
	CreatedByAPIKeyID int64
	CreatedAt         time.Time
	DeletedAt         *time.Time
}

type ListSkillsPageParams struct {
	WorkspaceID int64
	Limit       int
	Offset      int
}

type ListSkillVersionsPageParams struct {
	WorkspaceID     int64
	SkillExternalID string
	Limit           int
	Offset          int
}

func (d *DB) CreateSkillWithVersion(ctx context.Context, skill Skill, version SkillVersion) (Skill, SkillVersion, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	defer tx.Rollback(ctx)

	createdSkill, err := scanSkill(tx.QueryRow(ctx, `
		insert into skills (
			uuid, external_id, workspace_id, created_by_api_key_id,
			display_title, latest_version, source, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, 'custom', $7, $7)
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			display_title, latest_version, source, created_at, updated_at, deleted_at
	`, skill.UUID, skill.ExternalID, skill.WorkspaceID, skill.CreatedByAPIKeyID,
		skill.DisplayTitle, version.Version, skill.CreatedAt))
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}

	version.SkillID = createdSkill.ID
	version.SkillExternalID = createdSkill.ExternalID
	createdVersion, err := insertSkillVersion(ctx, tx, version)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Skill{}, SkillVersion{}, err
	}
	return createdSkill, createdVersion, nil
}

func (d *DB) CreateSkillVersion(ctx context.Context, workspaceID int64, skillExternalID string, version SkillVersion) (Skill, SkillVersion, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	defer tx.Rollback(ctx)

	skill, err := scanSkill(tx.QueryRow(ctx, skillSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, skillExternalID))
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if skill.Source != "custom" {
		return Skill{}, SkillVersion{}, ErrInvalidState
	}

	version.WorkspaceID = skill.WorkspaceID
	version.SkillID = skill.ID
	version.SkillExternalID = skill.ExternalID
	createdVersion, err := insertSkillVersion(ctx, tx, version)
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	updatedSkill, err := scanSkill(tx.QueryRow(ctx, `
		update skills
		set latest_version = $3,
			updated_at = $4
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			display_title, latest_version, source, created_at, updated_at, deleted_at
	`, workspaceID, skillExternalID, createdVersion.Version, createdVersion.CreatedAt))
	if err != nil {
		return Skill{}, SkillVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Skill{}, SkillVersion{}, err
	}
	return updatedSkill, createdVersion, nil
}

func (d *DB) GetSkill(ctx context.Context, workspaceID int64, externalID string) (Skill, error) {
	return scanSkill(d.Pool.QueryRow(ctx, skillSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) ListSkillsPage(ctx context.Context, params ListSkillsPageParams) ([]Skill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	rows, err := d.Pool.Query(ctx, skillSelectSQL()+`
		where workspace_id = $1 and deleted_at is null
		order by created_at desc, id desc
		limit $2 offset $3
	`, params.WorkspaceID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	skills, err := scanSkillRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(skills) > params.Limit
	if hasMore {
		skills = skills[:params.Limit]
	}
	return skills, hasMore, nil
}

func (d *DB) GetSkillVersion(ctx context.Context, workspaceID int64, skillExternalID, version string) (SkillVersion, error) {
	return scanSkillVersion(d.Pool.QueryRow(ctx, skillVersionSelectSQL()+`
			where workspace_id = $1
				and skill_external_id = $2
				and version = $3
				and deleted_at is null
		`, workspaceID, skillExternalID, version))
}

func (d *DB) GetLatestSkillVersion(ctx context.Context, workspaceID int64, skillExternalID string) (SkillVersion, error) {
	return scanSkillVersion(d.Pool.QueryRow(ctx, `
			select sv.id, sv.uuid::text, sv.external_id, sv.workspace_id, sv.skill_id, sv.skill_external_id,
				sv.version, sv.name, sv.description, sv.directory, sv.s3_bucket, sv.s3_key, sv.size_bytes, sv.sha256,
				sv.created_by_api_key_id, sv.created_at, sv.deleted_at
			from skills s
			join skill_versions sv
				on sv.skill_id = s.id
				and sv.version = s.latest_version
				and sv.deleted_at is null
			where s.workspace_id = $1
				and s.external_id = $2
				and s.deleted_at is null
				and s.latest_version is not null
				and s.latest_version <> ''
		`, workspaceID, skillExternalID))
}

func (d *DB) ListSkillVersionsPage(ctx context.Context, params ListSkillVersionsPageParams) ([]SkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	var skillID int64
	if err := d.Pool.QueryRow(ctx, `
		select id
		from skills
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, params.WorkspaceID, params.SkillExternalID).Scan(&skillID); errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	} else if err != nil {
		return nil, false, err
	}

	rows, err := d.Pool.Query(ctx, skillVersionSelectSQL()+`
		where skill_id = $1 and deleted_at is null
		order by created_at desc, id desc
		limit $2 offset $3
	`, skillID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	versions, err := scanSkillVersionRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) SoftDeleteSkill(ctx context.Context, workspaceID int64, externalID string) (Skill, []SkillVersion, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Skill{}, nil, err
	}
	defer tx.Rollback(ctx)

	skill, err := scanSkill(tx.QueryRow(ctx, skillSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID))
	if err != nil {
		return Skill{}, nil, err
	}

	rows, err := tx.Query(ctx, skillVersionSelectSQL()+`
		where skill_id = $1 and deleted_at is null
		order by created_at desc, id desc
	`, skill.ID)
	if err != nil {
		return Skill{}, nil, err
	}
	versions, err := scanSkillVersionRows(rows)
	rows.Close()
	if err != nil {
		return Skill{}, nil, err
	}

	if _, err := tx.Exec(ctx, `
		update skill_versions
		set deleted_at = now()
		where skill_id = $1 and deleted_at is null
	`, skill.ID); err != nil {
		return Skill{}, nil, err
	}
	deletedSkill, err := scanSkill(tx.QueryRow(ctx, `
		update skills
		set deleted_at = now(),
			updated_at = now()
		where id = $1 and deleted_at is null
		returning id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			display_title, latest_version, source, created_at, updated_at, deleted_at
	`, skill.ID))
	if err != nil {
		return Skill{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Skill{}, nil, err
	}
	return deletedSkill, versions, nil
}

func (d *DB) SoftDeleteSkillVersion(ctx context.Context, workspaceID int64, skillExternalID, version string) (SkillVersion, *string, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return SkillVersion{}, nil, err
	}
	defer tx.Rollback(ctx)

	skill, err := scanSkill(tx.QueryRow(ctx, skillSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, skillExternalID))
	if err != nil {
		return SkillVersion{}, nil, err
	}

	deletedVersion, err := scanSkillVersion(tx.QueryRow(ctx, `
		update skill_versions
		set deleted_at = now()
		where skill_id = $1 and version = $2 and deleted_at is null
		returning id, uuid::text, external_id, workspace_id, skill_id, skill_external_id,
			version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_id, created_at, deleted_at
	`, skill.ID, version))
	if err != nil {
		return SkillVersion{}, nil, err
	}

	var latestVersion *string
	var latest string
	err = tx.QueryRow(ctx, `
		select version
		from skill_versions
		where skill_id = $1 and deleted_at is null
		order by created_at desc, id desc
		limit 1
	`, skill.ID).Scan(&latest)
	if errors.Is(err, pgx.ErrNoRows) {
		latestVersion = nil
	} else if err != nil {
		return SkillVersion{}, nil, err
	} else {
		latestVersion = &latest
	}

	if _, err := tx.Exec(ctx, `
		update skills
		set latest_version = $2,
			updated_at = now()
		where id = $1
	`, skill.ID, latestVersion); err != nil {
		return SkillVersion{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SkillVersion{}, nil, err
	}
	return deletedVersion, latestVersion, nil
}

type skillTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertSkillVersion(ctx context.Context, tx skillTx, version SkillVersion) (SkillVersion, error) {
	return scanSkillVersion(tx.QueryRow(ctx, `
		insert into skill_versions (
			uuid, external_id, workspace_id, skill_id, skill_external_id, version,
			name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_id, created_at
		)
		values (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13,
			$14, $15
		)
		returning id, uuid::text, external_id, workspace_id, skill_id, skill_external_id,
			version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_id, created_at, deleted_at
	`, version.UUID, version.ExternalID, version.WorkspaceID, version.SkillID, version.SkillExternalID,
		version.Version, version.Name, version.Description, version.Directory, version.S3Bucket,
		version.S3Key, version.SizeBytes, version.SHA256, version.CreatedByAPIKeyID, version.CreatedAt))
}

func skillSelectSQL() string {
	return `
		select id, uuid::text, external_id, workspace_id, created_by_api_key_id,
			display_title, latest_version, source, created_at, updated_at, deleted_at
		from skills
	`
}

func skillVersionSelectSQL() string {
	return `
		select id, uuid::text, external_id, workspace_id, skill_id, skill_external_id,
			version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_id, created_at, deleted_at
		from skill_versions
	`
}

type skillScanner interface {
	Scan(dest ...any) error
}

type skillRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanSkill(row skillScanner) (Skill, error) {
	var skill Skill
	err := row.Scan(&skill.ID, &skill.UUID, &skill.ExternalID, &skill.WorkspaceID, &skill.CreatedByAPIKeyID,
		&skill.DisplayTitle, &skill.LatestVersion, &skill.Source, &skill.CreatedAt, &skill.UpdatedAt, &skill.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	return skill, err
}

func scanSkillRows(rows skillRows) ([]Skill, error) {
	var skills []Skill
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

func scanSkillVersion(row skillScanner) (SkillVersion, error) {
	var version SkillVersion
	err := row.Scan(&version.ID, &version.UUID, &version.ExternalID, &version.WorkspaceID, &version.SkillID, &version.SkillExternalID,
		&version.Version, &version.Name, &version.Description, &version.Directory, &version.S3Bucket, &version.S3Key,
		&version.SizeBytes, &version.SHA256, &version.CreatedByAPIKeyID, &version.CreatedAt, &version.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SkillVersion{}, ErrNotFound
	}
	return version, err
}

func scanSkillVersionRows(rows skillRows) ([]SkillVersion, error) {
	var versions []SkillVersion
	for rows.Next() {
		version, err := scanSkillVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}
