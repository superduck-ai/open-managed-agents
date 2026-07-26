package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// FilestoreSkillArchive is one immutable catalog zip projected below
// /skills in a Session filesystem.
type FilestoreSkillArchive struct {
	ID               int64
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	FilesystemUUID   string
	Source           string
	SkillVersionUUID string
	VirtualPath      string
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// FilestoreSkillArchiveInput contains resolved skill-version metadata. The
// archive remains owned by the skill catalog and is never charged to or
// deleted with the Session filesystem.
type FilestoreSkillArchiveInput struct {
	Source           string
	SkillVersionUUID string
	Directory        string
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
}

type filestoreSkillArchiveRow struct {
	ID               int64     `db:"id"`
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	FilesystemUUID   string    `db:"filesystem_uuid"`
	Source           string    `db:"source"`
	SkillVersionUUID string    `db:"skill_version_uuid"`
	VirtualPath      string    `db:"virtual_path"`
	S3Bucket         string    `db:"s3_bucket"`
	S3Key            string    `db:"s3_key"`
	SizeBytes        int64     `db:"size_bytes"`
	SHA256           string    `db:"sha256"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

var (
	filestoreSkillArchiveFilesystemQuery = filestoreFilesystemSelectSQL() + `
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and session_uuid = (
				select uuid
				from sessions
				where workspace_id = :workspace_id
					and external_id = :session_external_id
					and deleted_at is null
			)
			and deleted_at is null
		limit 1
		for update
	`
	filestoreSkillArchiveDeleteQuery = `
		delete from filestore_skill_archives
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
	`
	filestoreSkillArchiveInsertQuery = `
		insert into filestore_skill_archives (
			external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			source, skill_version_uuid, virtual_path, s3_bucket, s3_key,
			size_bytes, sha256, created_at, updated_at
		)
		values (
			concat('fsa_', replace(cast(gen_random_uuid() as text), '-', '')),
			CAST(:organization_uuid AS uuid),
			CAST(:workspace_uuid AS uuid),
			CAST(:filesystem_uuid AS uuid),
			:source,
			CAST(:skill_version_uuid AS uuid),
			:virtual_path,
			:s3_bucket,
			:s3_key,
			:size_bytes,
			:sha256,
			:now,
			:now
		)
	`
	filestoreSkillArchiveListQuery = `
		select
			id,
			cast(uuid as text) as uuid,
			external_id,
			cast(organization_uuid as text) as organization_uuid,
			cast(workspace_uuid as text) as workspace_uuid,
			cast(filesystem_uuid as text) as filesystem_uuid,
			source,
			cast(skill_version_uuid as text) as skill_version_uuid,
			virtual_path,
			s3_bucket,
			s3_key,
			size_bytes,
			sha256,
			created_at,
			updated_at
		from filestore_skill_archives
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and filesystem_uuid = (
				select uuid
				from filestore_filesystems
				where id = :filesystem_id
					and workspace_uuid = (
						select uuid from workspaces where id = :workspace_id
					)
					and deleted_at is null
			)
		order by virtual_path, id
	`
)

// ReplaceFilestoreSkillArchives atomically replaces the complete skill view for
// a public Session. Resolving "latest" happens before this call, so every row
// pins a concrete immutable version.
func (d *DB) ReplaceFilestoreSkillArchives(
	ctx context.Context,
	workspaceID int64,
	sessionExternalID string,
	archives []FilestoreSkillArchiveInput,
) error {
	if d == nil || d.sql == nil {
		return errors.New("database is unavailable")
	}
	for _, archive := range archives {
		if err := validateFilestoreSkillArchiveInput(archive); err != nil {
			return err
		}
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreSkillArchiveFilesystemQuery, map[string]any{
		"workspace_id":        workspaceID,
		"session_external_id": sessionExternalID,
	})
	if err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, provisionFilestoreNamespaceLockQuery, map[string]any{
		"filesystem_id": filesystem.ID,
	}); err != nil {
		return err
	}
	if err := ensureFilestoreFixedRootsTx(ctx, tx, workspaceID, filesystem, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, filestoreSkillArchiveDeleteQuery, map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
	}); err != nil {
		return err
	}

	seenPaths := make(map[string]struct{}, len(archives))
	seenVersions := make(map[string]struct{}, len(archives))
	now := time.Now().UTC()
	for _, archive := range archives {
		virtualPath := "/skills/" + archive.Directory
		versionKey := archive.Source + "\x00" + archive.SkillVersionUUID
		if _, exists := seenPaths[virtualPath]; exists {
			return fmt.Errorf("duplicate filestore skill path %q: %w", virtualPath, ErrDuplicate)
		}
		if _, exists := seenVersions[versionKey]; exists {
			continue
		}
		seenPaths[virtualPath] = struct{}{}
		seenVersions[versionKey] = struct{}{}
		if _, err := namedExecContext(ctx, tx, filestoreSkillArchiveInsertQuery, map[string]any{
			"organization_uuid":  filesystem.OrganizationUUID,
			"workspace_uuid":     filesystem.WorkspaceUUID,
			"filesystem_uuid":    filesystem.UUID,
			"source":             archive.Source,
			"skill_version_uuid": archive.SkillVersionUUID,
			"virtual_path":       virtualPath,
			"s3_bucket":          archive.S3Bucket,
			"s3_key":             archive.S3Key,
			"size_bytes":         archive.SizeBytes,
			"sha256":             strings.ToLower(archive.SHA256),
			"now":                now,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListFilestoreSkillArchives returns the complete deterministic projection set
// for virtual namespace resolution.
func (d *DB) ListFilestoreSkillArchives(
	ctx context.Context,
	workspaceID int64,
	filesystemID int64,
) ([]FilestoreSkillArchive, error) {
	var rows []filestoreSkillArchiveRow
	err := namedSelectContext(ctx, d.sql, &rows, filestoreSkillArchiveListQuery, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": filesystemID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return []FilestoreSkillArchive{}, nil
	}
	if err != nil {
		return nil, err
	}
	archives := make([]FilestoreSkillArchive, 0, len(rows))
	for _, row := range rows {
		archives = append(archives, row.archive())
	}
	return archives, nil
}

func validateFilestoreSkillArchiveInput(input FilestoreSkillArchiveInput) error {
	if input.Source != "anthropic" && input.Source != "custom" {
		return fmt.Errorf("unsupported skill source %q", input.Source)
	}
	directory := strings.TrimSpace(input.Directory)
	if directory == "" || strings.ContainsAny(directory, "/\\\x00") || directory == "." || directory == ".." {
		return fmt.Errorf("invalid skill directory %q", input.Directory)
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.SkillVersionUUID)); err != nil {
		return fmt.Errorf("invalid skill version UUID: %w", err)
	}
	checksum := strings.TrimSpace(input.SHA256)
	decodedChecksum, checksumErr := hex.DecodeString(checksum)
	if strings.TrimSpace(input.S3Bucket) == "" ||
		strings.TrimSpace(input.S3Key) == "" ||
		input.SizeBytes <= 0 ||
		checksumErr != nil ||
		len(decodedChecksum) != 32 {
		return ErrInvalidState
	}
	return nil
}

func (row filestoreSkillArchiveRow) archive() FilestoreSkillArchive {
	return FilestoreSkillArchive{
		ID:               row.ID,
		UUID:             row.UUID,
		ExternalID:       row.ExternalID,
		OrganizationUUID: row.OrganizationUUID,
		WorkspaceUUID:    row.WorkspaceUUID,
		FilesystemUUID:   row.FilesystemUUID,
		Source:           row.Source,
		SkillVersionUUID: row.SkillVersionUUID,
		VirtualPath:      row.VirtualPath,
		S3Bucket:         row.S3Bucket,
		S3Key:            row.S3Key,
		SizeBytes:        row.SizeBytes,
		SHA256:           row.SHA256,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}
