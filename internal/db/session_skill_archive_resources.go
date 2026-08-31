package db

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
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

// ReplaceSessionSkillArchiveResources 原子替换一个公开 Session 的完整
// /skills Skill Archive Resource 集合。每个 Skill Archive 由一条 File 快照承载。
func (d *DB) ReplaceSessionSkillArchiveResources(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	inputs []SessionSkillArchiveResourceInput,
) error {
	if d == nil || d.mapperDB == nil {
		return errors.New("database is unavailable")
	}
	entries, err := normalizeSessionSkillArchiveResources(inputs)
	if err != nil {
		return err
	}
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return replaceSessionSkillArchiveResourcesTx(
			ctx,
			executor,
			workspaceUUID,
			sessionExternalID,
			entries,
		)
	})
}

func replaceSessionSkillArchiveResourcesTx(
	ctx context.Context,
	executor yourbatis.Executor,
	workspaceUUID string,
	sessionExternalID string,
	entries []normalizedSessionSkillArchiveResource,
) error {
	filesystemMapper := NewFilestoreFilesystemMapper(executor)
	filesystemRow, found, err := filesystemMapper.FindSessionFilesystemByExternalID(
		ctx,
		workspaceUUID,
		sessionExternalID,
	)
	if err != nil {
		return fmt.Errorf("load Session filesystem for skill archives: %w", err)
	}
	if !found {
		return fmt.Errorf("load Session filesystem for skill archives: %w", ErrNotFound)
	}
	filesystem, err := filesystemRow.filesystem()
	if err != nil {
		return fmt.Errorf("load Session filesystem for skill archives: %w", err)
	}
	if err := filesystemMapper.LockFilesystem(ctx, filesystem.UUID); err != nil {
		return fmt.Errorf("lock Session filesystem for skill archives: %w", err)
	}
	now := time.Now().UTC()
	if err := ensureFilestoreFixedRootsTx(ctx, executor, filesystem, now); err != nil {
		return fmt.Errorf("ensure Filestore roots for skill archives: %w", err)
	}
	retireParams := sessionSkillArchiveRetireParams{
		WorkspaceUUID: workspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		Now:           now,
	}
	fileMapper := NewFileMapper(executor)
	resourceMapper := NewSessionResourceMapper(executor)
	if err := fileMapper.RetireSkillArchiveFiles(ctx, retireParams); err != nil {
		return fmt.Errorf("retire Session skill archive Files: %w", err)
	}
	if err := resourceMapper.RetireSkillArchiveResources(ctx, retireParams); err != nil {
		return fmt.Errorf("retire Session skill archives: %w", err)
	}

	for _, entry := range entries {
		if err := validateFilestoreSkillArchiveVersionTx(ctx, executor, workspaceUUID, entry); err != nil {
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
		params := sessionSkillArchiveInsertParams{
			FileUUID:           fileUUID,
			FileExternalID:     fileExternalID,
			ResourceUUID:       resourceUUID,
			ResourceExternalID: resourceExternalID,
			WorkspaceUUID:      workspaceUUID,
			SessionUUID:        filesystem.SessionUUID,
			EntryPath:          entry.Path,
			Filename:           entry.Filename,
			Source:             entry.Source,
			SizeBytes:          entry.SizeBytes,
			SHA256:             entry.SHA256,
			S3Bucket:           entry.S3Bucket,
			S3Key:              entry.S3Key,
			Now:                now,
		}
		if err := fileMapper.InsertSkillArchiveFile(ctx, params); err != nil {
			return fmt.Errorf("insert Session skill archive File %s: %w", entry.Path, err)
		}
		if err := resourceMapper.InsertSkillArchiveResource(ctx, params); err != nil {
			return fmt.Errorf("insert Session skill archive %s: %w", entry.Path, err)
		}
	}
	return nil
}

func validateFilestoreSkillArchiveVersionTx(
	ctx context.Context,
	tx yourbatis.Executor,
	workspaceUUID string,
	entry normalizedSessionSkillArchiveResource,
) error {
	mapper := NewSkillVersionMapper(tx)
	valid, err := mapper.ValidateSkillArchiveVersion(ctx, sessionSkillArchiveValidationParams{
		Source:        entry.Source,
		WorkspaceUUID: workspaceUUID,
		VersionUUID:   entry.SkillVersionUUID,
		Directory:     strings.TrimPrefix(entry.Path, "/skills/"),
		S3Bucket:      entry.S3Bucket,
		S3Key:         entry.S3Key,
		SizeBytes:     entry.SizeBytes,
		SHA256:        entry.SHA256,
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
	mapper := NewSessionResourceFileMapper(d.mapperDB)
	rows, err := mapper.ListSkillArchiveResources(
		ctx,
		workspaceUUID,
		filesystemUUID,
	)
	if err != nil {
		return nil, err
	}
	return sessionResourceFilesFromMapperRows(rows)
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
