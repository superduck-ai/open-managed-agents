package db

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type normalizedFilestoreSkillArchiveEntry struct {
	Source           string
	SkillVersionUUID string
	Path             string
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
}

var (
	filestoreSkillArchiveEntryFilesystemQuery = filestoreFilesystemSelectSQL() + `
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
	filestoreSkillArchiveEntryDeleteQuery = `
		delete from filestore_entries
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and kind = 'archive'
			and managed_by = 'skill_archive'
	`
	filestoreSkillArchiveEntryInsertQuery = `
		insert into filestore_entries (
			uuid,
			external_id,
			organization_uuid,
			workspace_uuid,
			filesystem_uuid,
			kind,
			path,
			parent_path,
			size_bytes,
			media_type,
			detected_mime_type,
			metadata,
			authorization_metadata,
			downloadable,
			sha256,
			s3_bucket,
			s3_key,
			managed_by,
			managed_resource_uuid,
			created_by_api_key_uuid,
			created_by_session_uuid,
			created_by_code_session_uuid,
			created_at,
			updated_at
		)
		values (
			gen_random_uuid(),
			concat('fse_', replace(cast(gen_random_uuid() as text), '-', '')),
			CAST(:organization_uuid AS uuid),
			CAST(:workspace_uuid AS uuid),
			CAST(:filesystem_uuid AS uuid),
			'archive',
			:entry_path,
			'/skills',
			:size_bytes,
			'application/zip',
			'application/zip',
			jsonb_build_object('skill_source', cast(:source as text)),
			cast('{}' as jsonb),
			false,
			:sha256,
			:s3_bucket,
			:s3_key,
			'skill_archive',
			CAST(:skill_version_uuid AS uuid),
			CAST(:created_by_api_key_uuid AS uuid),
			CAST(:created_by_session_uuid AS uuid),
			CAST(:created_by_code_session_uuid AS uuid),
			:now,
			:now
		)
	`
	filestoreSkillArchiveEntryListQuery = filestoreEntrySelectSQL() + `
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
			and kind = 'archive'
			and managed_by = 'skill_archive'
			and deleted_at is null
		order by path, id
	`
)

// ReplaceFilestoreSkillArchiveEntries 原子替换一个公开 Session 的完整
// /skills archive entry 集合。调用前必须已把 "latest" 解析为具体版本。
func (d *DB) ReplaceFilestoreSkillArchiveEntries(
	ctx context.Context,
	workspaceID int64,
	sessionExternalID string,
	inputs []FilestoreSkillArchiveEntryInput,
) error {
	if d == nil || d.sql == nil {
		return errors.New("database is unavailable")
	}
	entries, err := normalizeFilestoreSkillArchiveEntries(inputs)
	if err != nil {
		return err
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreSkillArchiveEntryFilesystemQuery, map[string]any{
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
	now := time.Now().UTC()
	if err := ensureFilestoreFixedRootsTx(ctx, tx, workspaceID, filesystem, now); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, filestoreSkillArchiveEntryDeleteQuery, map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
	}); err != nil {
		return err
	}

	for _, entry := range entries {
		if _, err := namedExecContext(ctx, tx, filestoreSkillArchiveEntryInsertQuery, map[string]any{
			"organization_uuid":            filesystem.OrganizationUUID,
			"workspace_uuid":               filesystem.WorkspaceUUID,
			"filesystem_uuid":              filesystem.UUID,
			"entry_path":                   entry.Path,
			"source":                       entry.Source,
			"skill_version_uuid":           entry.SkillVersionUUID,
			"s3_bucket":                    entry.S3Bucket,
			"s3_key":                       entry.S3Key,
			"size_bytes":                   entry.SizeBytes,
			"sha256":                       entry.SHA256,
			"created_by_api_key_uuid":      filesystem.CreatedByAPIKeyUUID,
			"created_by_session_uuid":      filesystem.SessionUUID,
			"created_by_code_session_uuid": filesystem.CodeSessionUUID,
			"now":                          now,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListFilestoreSkillArchiveEntries 返回一个 Session 文件系统中完整且稳定排序的
// archive entry 集合，供只读 /skills 虚拟视图解析。
func (d *DB) ListFilestoreSkillArchiveEntries(
	ctx context.Context,
	workspaceID int64,
	filesystemID int64,
) ([]FilestoreEntry, error) {
	var rows []filestoreEntryRow
	if err := namedSelectContext(ctx, d.sql, &rows, filestoreSkillArchiveEntryListQuery, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": filesystemID,
	}); err != nil {
		return nil, err
	}
	return filestoreEntriesFromSQLXRows(rows)
}

func normalizeFilestoreSkillArchiveEntries(
	inputs []FilestoreSkillArchiveEntryInput,
) ([]normalizedFilestoreSkillArchiveEntry, error) {
	entries := make([]normalizedFilestoreSkillArchiveEntry, 0, len(inputs))
	seenPaths := make(map[string]struct{}, len(inputs))
	seenVersions := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		entry, err := normalizeFilestoreSkillArchiveEntry(input)
		if err != nil {
			return nil, err
		}
		if _, exists := seenPaths[entry.Path]; exists {
			return nil, fmt.Errorf("duplicate filestore skill path %q: %w", entry.Path, ErrDuplicate)
		}
		if _, exists := seenVersions[entry.SkillVersionUUID]; exists {
			return nil, fmt.Errorf(
				"duplicate filestore skill version %q: %w",
				entry.SkillVersionUUID,
				ErrDuplicate,
			)
		}
		seenPaths[entry.Path] = struct{}{}
		seenVersions[entry.SkillVersionUUID] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, nil
}

func normalizeFilestoreSkillArchiveEntry(
	input FilestoreSkillArchiveEntryInput,
) (normalizedFilestoreSkillArchiveEntry, error) {
	source := strings.TrimSpace(input.Source)
	if source != "anthropic" && source != "custom" {
		return normalizedFilestoreSkillArchiveEntry{}, fmt.Errorf("unsupported skill source %q", input.Source)
	}
	directory := strings.TrimSpace(input.Directory)
	if directory == "" || strings.ContainsAny(directory, "/\\\x00") || directory == "." || directory == ".." {
		return normalizedFilestoreSkillArchiveEntry{}, fmt.Errorf("invalid skill directory %q", input.Directory)
	}
	entryPath := "/skills/" + directory
	if err := validateFilestorePath(entryPath); err != nil {
		return normalizedFilestoreSkillArchiveEntry{}, fmt.Errorf("invalid skill directory %q: %w", input.Directory, err)
	}
	versionUUID := strings.TrimSpace(input.SkillVersionUUID)
	if _, err := uuid.Parse(versionUUID); err != nil {
		return normalizedFilestoreSkillArchiveEntry{}, fmt.Errorf("invalid skill version UUID: %w", err)
	}
	checksum := strings.ToLower(strings.TrimSpace(input.SHA256))
	decodedChecksum, checksumErr := hex.DecodeString(checksum)
	if strings.TrimSpace(input.S3Bucket) == "" ||
		strings.TrimSpace(input.S3Key) == "" ||
		input.SizeBytes <= 0 ||
		checksumErr != nil ||
		len(decodedChecksum) != 32 {
		return normalizedFilestoreSkillArchiveEntry{}, ErrInvalidState
	}
	return normalizedFilestoreSkillArchiveEntry{
		Source:           source,
		SkillVersionUUID: versionUUID,
		Path:             entryPath,
		S3Bucket:         strings.TrimSpace(input.S3Bucket),
		S3Key:            strings.TrimSpace(input.S3Key),
		SizeBytes:        input.SizeBytes,
		SHA256:           checksum,
	}, nil
}
