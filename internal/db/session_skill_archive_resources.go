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
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
}

var (
	sessionSkillArchiveResourceFilesystemQuery = filestoreFilesystemSelectSQL() + `
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
	sessionSkillArchiveResourceRetireQuery = `
		update session_resources
		set deleted_at = :now, updated_at = :now
		where workspace_id = :workspace_id
			and session_id = :session_id
			and resource_type = 'skill_archive'
			and deleted_at is null
	`
	sessionSkillArchiveResourceInsertQuery = `
		insert into session_resources (
			uuid, external_id, organization_id, workspace_id, session_id,
			session_external_id, resource_type, payload, secret_payload,
			path, parent_path, skill_version_uuid, created_at, updated_at
		)
		select CAST(:resource_uuid AS uuid),
			:resource_external_id,
			session.organization_id, session.workspace_id, session.id,
			session.external_id, 'skill_archive', null, null,
			:entry_path, '/skills', CAST(:skill_version_uuid AS uuid), :now, :now
		from sessions session
		where session.id = :session_id and session.workspace_id = :workspace_id
			and session.deleted_at is null
	`
	sessionSkillArchiveResourceListQuery = sessionResourceFileSelectSQL() + `
		where workspace_uuid = (select cast(uuid as text) from workspaces where id = :workspace_id)
			and filesystem_uuid = (
				select cast(uuid as text)
				from filestore_filesystems
				where id = :filesystem_id
					and workspace_uuid = (
						select uuid from workspaces where id = :workspace_id
					)
					and deleted_at is null
			)
			and kind = 'archive'
			and skill_version_uuid is not null
			and deleted_at is null
		order by path, id
	`
)

// ReplaceSessionSkillArchiveResources 原子替换一个公开 Session 的完整
// /skills Skill Archive Resource 集合。调用前必须已把 "latest" 解析为具体版本。
func (d *DB) ReplaceSessionSkillArchiveResources(
	ctx context.Context,
	workspaceID int64,
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
		"workspace_id":        workspaceID,
		"session_external_id": sessionExternalID,
	})
	if err != nil {
		return fmt.Errorf("load Session filesystem for skill archives: %w", err)
	}
	if _, err := namedExecContext(ctx, tx, provisionFilestoreNamespaceLockQuery, map[string]any{
		"filesystem_id": filesystem.ID,
	}); err != nil {
		return fmt.Errorf("lock Session filesystem for skill archives: %w", err)
	}
	now := time.Now().UTC()
	if err := ensureFilestoreFixedRootsTx(ctx, tx, workspaceID, filesystem, now); err != nil {
		return fmt.Errorf("ensure Filestore roots for skill archives: %w", err)
	}
	if _, err := namedExecContext(ctx, tx, sessionSkillArchiveResourceRetireQuery, map[string]any{
		"workspace_id": workspaceID,
		"session_id":   filesystem.SessionID,
		"now":          now,
	}); err != nil {
		return fmt.Errorf("retire Session skill archives: %w", err)
	}

	for _, entry := range entries {
		if err := validateFilestoreSkillArchiveVersionTx(ctx, tx, workspaceID, entry); err != nil {
			return err
		}
		resourceUUID, resourceExternalID, err := newSessionResourceIdentity()
		if err != nil {
			return fmt.Errorf("generate Session skill archive identity: %w", err)
		}
		if _, err := namedExecContext(ctx, tx, sessionSkillArchiveResourceInsertQuery, map[string]any{
			"resource_uuid":        resourceUUID,
			"resource_external_id": resourceExternalID,
			"workspace_id":         workspaceID,
			"session_id":           filesystem.SessionID,
			"entry_path":           entry.Path,
			"skill_version_uuid":   entry.SkillVersionUUID,
			"now":                  now,
		}); err != nil {
			return fmt.Errorf("insert Session skill archive %s: %w", entry.SkillVersionUUID, err)
		}
	}
	return tx.Commit()
}

func validateFilestoreSkillArchiveVersionTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	entry normalizedSessionSkillArchiveResource,
) error {
	var valid bool
	err := namedGetContext(ctx, tx, &valid, `
		select case CAST(:source AS text)
			when 'custom' then exists (
				select 1 from skill_versions version
				where version.workspace_id = :workspace_id
					and version.uuid = CAST(:version_uuid AS uuid)
					and version.directory = :directory
					and version.s3_bucket = :s3_bucket
					and version.s3_key = :s3_key
					and version.size_bytes = :size_bytes
					and version.sha256 = :sha256
					and version.deleted_at is null
			)
			when 'anthropic' then exists (
				select 1 from builtin_skill_versions version
				where version.uuid = CAST(:version_uuid AS uuid)
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
		"source":       entry.Source,
		"workspace_id": workspaceID,
		"version_uuid": entry.SkillVersionUUID,
		"directory":    strings.TrimPrefix(entry.Path, "/skills/"),
		"s3_bucket":    entry.S3Bucket,
		"s3_key":       entry.S3Key,
		"size_bytes":   entry.SizeBytes,
		"sha256":       entry.SHA256,
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
	workspaceID int64,
	filesystemID int64,
) ([]SessionResourceFile, error) {
	var rows []sessionResourceFileRow
	if err := namedSelectContext(ctx, d.sql, &rows, sessionSkillArchiveResourceListQuery, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": filesystemID,
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
		S3Bucket:         strings.TrimSpace(input.S3Bucket),
		S3Key:            strings.TrimSpace(input.S3Key),
		SizeBytes:        input.SizeBytes,
		SHA256:           checksum,
	}, nil
}
