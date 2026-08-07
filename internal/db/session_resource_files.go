package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

const (
	defaultSessionResourceFilesPageLimit = 100
	maxSessionResourceFilesPageLimit     = 1000
)

// GetSessionResourceFile 在工作区与文件系统双重边界内读取一个有效节点。
// 根目录不落表，由文件系统记录即时投影为虚拟目录。
func (d *DB) GetSessionResourceFile(ctx context.Context, workspaceUUID, filesystemUUID string, entryPath string) (SessionResourceFile, error) {
	if err := validateFilestorePath(entryPath); err != nil {
		return SessionResourceFile{}, err
	}
	filesystemMapper := NewFilestoreFilesystemMapper(d.mapperDB)
	filesystemRow, found, err := filesystemMapper.FindFilesystemByUUID(
		ctx,
		workspaceUUID,
		filesystemUUID,
	)
	if err != nil {
		return SessionResourceFile{}, err
	}
	if !found {
		return SessionResourceFile{}, ErrNotFound
	}
	filesystem, err := filesystemRow.filesystem()
	if err != nil {
		return SessionResourceFile{}, err
	}
	if entryPath == "/" {
		return virtualFilestoreRoot(filesystem), nil
	}
	return getActiveSessionResourceFile(ctx, d.mapperDB, filesystem, entryPath)
}

// ListSessionResourceFilesPage 以 (path, id) 为稳定排序键执行键集分页。
// 过期或软删除节点不会出现在结果中。
func (d *DB) ListSessionResourceFilesPage(ctx context.Context, params ListSessionResourceFilesPageParams) (SessionResourceFilePage, error) {
	if err := validateFilestorePath(params.DirectoryPath); err != nil {
		return SessionResourceFilePage{}, err
	}
	params.Limit = normalizeSessionResourceFilesPageLimit(params.Limit)
	filesystem, err := d.resolveFilestoreDirectoryForRead(ctx, params.WorkspaceUUID, params.FilesystemUUID, params.DirectoryPath)
	if err != nil {
		return SessionResourceFilePage{}, err
	}
	mapperParams := sessionResourceFilePageMapperParams{
		WorkspaceUUID:   filesystem.WorkspaceUUID,
		SessionUUID:     filesystem.SessionUUID,
		DirectoryPath:   params.DirectoryPath,
		DirectoryPrefix: filestoreDirectoryPrefix(params.DirectoryPath),
		FetchLimit:      params.Limit + 1,
		Recursive:       params.Recursive,
		HasCursor:       params.Cursor != nil,
	}
	if params.Cursor != nil {
		mapperParams.CursorPath = params.Cursor.Path
		mapperParams.CursorUUID = params.Cursor.UUID
	}
	mapper := NewSessionResourceFileMapper(d.mapperDB)
	rows, err := mapper.ListResourceFilesPage(ctx, mapperParams)
	if err != nil {
		return SessionResourceFilePage{}, err
	}
	entries, err := sessionResourceFilesFromMapperRows(rows)
	if err != nil {
		return SessionResourceFilePage{}, err
	}
	return newSessionResourceFilePage(entries, params.Limit), nil
}

func normalizeSessionResourceFilesPageLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultSessionResourceFilesPageLimit
	case limit > maxSessionResourceFilesPageLimit:
		return maxSessionResourceFilesPageLimit
	default:
		return limit
	}
}

func filestoreDirectoryPrefix(directoryPath string) string {
	if directoryPath == "/" {
		return directoryPath
	}
	return directoryPath + "/"
}

func newSessionResourceFilePage(entries []SessionResourceFile, limit int) SessionResourceFilePage {
	page := SessionResourceFilePage{Entries: entries, HasMore: len(entries) > limit}
	if page.HasMore {
		page.Entries = entries[:limit]
	}
	return page
}

func getActiveSessionResourceFile(
	ctx context.Context,
	database yourbatis.Executor,
	filesystem FilestoreFilesystem,
	entryPath string,
) (SessionResourceFile, error) {
	mapper := NewSessionResourceFileMapper(database)
	row, found, err := mapper.FindActiveResourceFile(ctx, sessionResourcePathParams{
		WorkspaceUUID: filesystem.WorkspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		EntryPath:     entryPath,
	})
	if err != nil {
		return SessionResourceFile{}, err
	}
	if !found {
		return SessionResourceFile{}, ErrNotFound
	}
	return row.entry()
}
