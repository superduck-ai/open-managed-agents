package db

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type normalizedSessionSkillArchiveResource struct {
	Source           string
	SkillVersionUUID string
	Path             string
	Filename         string
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
}

var (
	sessionSkillArchiveResourceFilesystemQuery = filestoreFilesystemSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and session_uuid = (
				select uuid
				from sessions
				where workspace_uuid = :workspace_uuid
					and external_id = :session_external_id
					and deleted_at is null
			)
			and deleted_at is null
		limit 1
		for update
	`
	sessionSkillArchiveResourceRetireQuery = `
		update session_resources
		set deleted_at = :now, updated_at = :now
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and resource_type = 'skill_archive'
			and deleted_at is null
	`
	sessionSkillArchiveFileRetireQuery = `
		update files file
		set deleted_at = :now
		where file.workspace_uuid = :workspace_uuid
			and file.deleted_at is null
			and file.uuid in (
			select resource.file_uuid
				from session_resources resource
				where resource.workspace_uuid = (
					:workspace_uuid
				)
					and resource.session_uuid = :session_uuid
					and resource.resource_type = 'skill_archive'
					and resource.file_uuid is not null
					and resource.deleted_at is null
			)
	`
	sessionSkillArchiveFileInsertQuery = `
		insert into files (
			uuid, external_id, workspace_uuid, filename, mime_type, detected_mime_type,
			size_bytes, metadata, authorization_metadata, tags, downloadable,
			sha256, s3_bucket, s3_key, created_by_api_key_uuid, created_at
		)
		select :file_uuid, :file_external_id, session.workspace_uuid,
			:filename, 'application/zip', 'application/zip', :size_bytes,
			jsonb_build_object('skill_source', CAST(:source AS text)),
			CAST('{}' AS jsonb), CAST(ARRAY[] AS text[]), false,
			:sha256, :s3_bucket, :s3_key, session.created_by_api_key_uuid, :now
		from sessions session
		where session.uuid = :session_uuid
			and session.workspace_uuid = :workspace_uuid
			and session.deleted_at is null
	`
	sessionSkillArchiveResourceInsertQuery = `
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, resource_type, payload, secret_payload,
			path, parent_path, file_uuid, created_at, updated_at
		)
		select :resource_uuid,
			:resource_external_id,
			session.organization_uuid,
			session.workspace_uuid, session.uuid,
			session.external_id, 'skill_archive', null, null,
			:entry_path, '/skills', :file_uuid, :now, :now
		from sessions session
		where session.uuid = :session_uuid
			and session.workspace_uuid = :workspace_uuid
			and session.deleted_at is null
	`
	sessionSkillArchiveResourceListQuery = sessionResourceFileSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and session_uuid = (
				select session_uuid
				from filestore_filesystems
				where uuid = :filesystem_uuid
					and workspace_uuid = (
						:workspace_uuid
					)
					and deleted_at is null
			)
			and kind = 'archive'
			and deleted_at is null
		order by path, id
	`
)

// ReplaceSessionSkillArchiveResources 原子替换一个公开 Session 的完整
// /skills Skill Archive Resource 集合。每个 Skill Archive 由一条 File 快照承载。
func (d *DB) ReplaceSessionSkillArchiveResources(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	inputs []SessionSkillArchiveResourceInput,
) error {
	if d == nil || d.sql == nil {
		return errors.New("database is unavailable")
	}
	entries, err := normalizeSessionSkillArchiveResources(inputs)
	if err != nil {
		return err
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, sessionSkillArchiveResourceFilesystemQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
	})
	if err != nil {
		return fmt.Errorf("load Session filesystem for skill archives: %w", err)
	}
	if _, err := namedExecContext(ctx, tx, provisionFilestoreNamespaceLockQuery, map[string]any{
		"filesystem_uuid": dbUUID(filesystem.UUID),
	}); err != nil {
		return fmt.Errorf("lock Session filesystem for skill archives: %w", err)
	}
	now := time.Now().UTC()
	if err := ensureFilestoreFixedRootsTx(ctx, tx, filesystem, now); err != nil {
		return fmt.Errorf("ensure Filestore roots for skill archives: %w", err)
	}
	retireArguments := map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"session_uuid":   dbUUID(filesystem.SessionUUID),
		"now":            now,
	}
	if _, err := namedExecContext(ctx, tx, sessionSkillArchiveFileRetireQuery, retireArguments); err != nil {
		return fmt.Errorf("retire Session skill archive Files: %w", err)
	}
	if _, err := namedExecContext(ctx, tx, sessionSkillArchiveResourceRetireQuery, retireArguments); err != nil {
		return fmt.Errorf("retire Session skill archives: %w", err)
	}

	for _, entry := range entries {
		if err := validateFilestoreSkillArchiveVersionTx(ctx, tx, workspaceUUID, entry); err != nil {
			return err
		}
		fileUUID, fileExternalID, err := newFileIdentity()
		if err != nil {
			return fmt.Errorf("generate Session skill archive File identity: %w", err)
		}
		resourceUUID, resourceExternalID, err := newSessionResourceIdentity()
		if err != nil {
			return fmt.Errorf("generate Session skill archive identity: %w", err)
		}
		arguments := map[string]any{
			"file_uuid":            dbUUID(fileUUID),
			"file_external_id":     fileExternalID,
			"resource_uuid":        dbUUID(resourceUUID),
			"resource_external_id": resourceExternalID,
			"workspace_uuid":       dbUUID(workspaceUUID),
			"session_uuid":         dbUUID(filesystem.SessionUUID),
			"entry_path":           entry.Path,
			"filename":             entry.Filename,
			"source":               entry.Source,
			"size_bytes":           entry.SizeBytes,
			"sha256":               entry.SHA256,
			"s3_bucket":            entry.S3Bucket,
			"s3_key":               entry.S3Key,
			"now":                  now,
		}
		if _, err := namedExecContext(ctx, tx, sessionSkillArchiveFileInsertQuery, arguments); err != nil {
			return fmt.Errorf("insert Session skill archive File %s: %w", entry.Path, err)
		}
		if _, err := namedExecContext(ctx, tx, sessionSkillArchiveResourceInsertQuery, arguments); err != nil {
			return fmt.Errorf("insert Session skill archive %s: %w", entry.Path, err)
		}
	}
	return tx.Commit()
}

func validateFilestoreSkillArchiveVersionTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	entry normalizedSessionSkillArchiveResource,
) error {
	var valid bool
	err := namedGetContext(ctx, tx, &valid, `
		select case CAST(:source AS text)
			when 'custom' then exists (
				select 1 from skill_versions version
				where version.workspace_uuid = :workspace_uuid
					and version.uuid = :version_uuid
					and version.directory = :directory
					and version.s3_bucket = :s3_bucket
					and version.s3_key = :s3_key
					and version.size_bytes = :size_bytes
					and version.sha256 = :sha256
					and version.deleted_at is null
			)
			when 'anthropic' then exists (
				select 1 from builtin_skill_versions version
				where version.uuid = :version_uuid
					and version.directory = :directory
					and version.s3_bucket = :s3_bucket
					and version.s3_key = :s3_key
					and version.size_bytes = :size_bytes
					and version.sha256 = :sha256
					and version.deleted_at is null
			)
			else false
		end
	`, map[string]any{
		"source":         entry.Source,
		"workspace_uuid": dbUUID(workspaceUUID),
		"version_uuid":   dbUUID(entry.SkillVersionUUID),
		"directory":      strings.TrimPrefix(entry.Path, "/skills/"),
		"s3_bucket":      entry.S3Bucket,
		"s3_key":         entry.S3Key,
		"size_bytes":     entry.SizeBytes,
		"sha256":         entry.SHA256,
	})
	if err != nil {
		return fmt.Errorf("validate Session skill archive %s: %w", entry.SkillVersionUUID, err)
	}
	if !valid {
		return fmt.Errorf("validate Session skill archive %s: %w", entry.SkillVersionUUID, ErrInvalidState)
	}
	return nil
}

// ListSessionSkillArchiveResources 返回一个 Session 文件系统中完整且稳定排序的
// Skill Archive Resource 集合，供只读 /skills 虚拟视图解析。
func (d *DB) ListSessionSkillArchiveResources(
	ctx context.Context,
	workspaceUUID string,
	filesystemUUID string,
) ([]SessionResourceFile, error) {
	var rows []sessionResourceFileRow
	if err := namedSelectContext(ctx, d.sql, &rows, sessionSkillArchiveResourceListQuery, map[string]any{
		"workspace_uuid":  dbUUID(workspaceUUID),
		"filesystem_uuid": dbUUID(filesystemUUID),
	}); err != nil {
		return nil, err
	}
	return sessionResourceFilesFromSQLXRows(rows)
}

func normalizeSessionSkillArchiveResources(
	inputs []SessionSkillArchiveResourceInput,
) ([]normalizedSessionSkillArchiveResource, error) {
	entries := make([]normalizedSessionSkillArchiveResource, 0, len(inputs))
	seenPaths := make(map[string]struct{}, len(inputs))
	seenVersions := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		entry, err := normalizeSessionSkillArchiveResource(input)
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

func normalizeSessionSkillArchiveResource(
	input SessionSkillArchiveResourceInput,
) (normalizedSessionSkillArchiveResource, error) {
	source := strings.TrimSpace(input.Source)
	if source != "anthropic" && source != "custom" {
		return normalizedSessionSkillArchiveResource{}, fmt.Errorf("unsupported skill source %q", input.Source)
	}
	directory := strings.TrimSpace(input.Directory)
	if directory == "" || strings.ContainsAny(directory, "/\\\x00") || directory == "." || directory == ".." {
		return normalizedSessionSkillArchiveResource{}, fmt.Errorf("invalid skill directory %q", input.Directory)
	}
	entryPath := "/skills/" + directory
	if err := validateFilestorePath(entryPath); err != nil {
		return normalizedSessionSkillArchiveResource{}, fmt.Errorf("invalid skill directory %q: %w", input.Directory, err)
	}
	versionUUID := strings.TrimSpace(input.SkillVersionUUID)
	if _, err := uuid.Parse(versionUUID); err != nil {
		return normalizedSessionSkillArchiveResource{}, fmt.Errorf("invalid skill version UUID: %w", err)
	}
	checksum := strings.ToLower(strings.TrimSpace(input.SHA256))
	decodedChecksum, checksumErr := hex.DecodeString(checksum)
	if strings.TrimSpace(input.S3Bucket) == "" ||
		strings.TrimSpace(input.S3Key) == "" ||
		input.SizeBytes <= 0 ||
		checksumErr != nil ||
		len(decodedChecksum) != 32 {
		return normalizedSessionSkillArchiveResource{}, ErrInvalidState
	}
	return normalizedSessionSkillArchiveResource{
		Source:           source,
		SkillVersionUUID: versionUUID,
		Path:             entryPath,
		Filename:         directory + ".zip",
		S3Bucket:         strings.TrimSpace(input.S3Bucket),
		S3Key:            strings.TrimSpace(input.S3Key),
		SizeBytes:        input.SizeBytes,
		SHA256:           checksum,
	}, nil
}
