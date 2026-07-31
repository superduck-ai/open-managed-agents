package db

import "context"

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
	filesystem, err := getFilestoreFilesystemByUUIDSQLX(ctx, d.sql, workspaceUUID, filesystemUUID)
	if err != nil {
		return SessionResourceFile{}, err
	}
	if entryPath == "/" {
		return virtualFilestoreRoot(filesystem), nil
	}
	return getActiveSessionResourceFileSQLX(ctx, d.sql, filesystem, entryPath)
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
	query, args := buildSessionResourceFilesPageQuery(filesystem, params)
	var rows []sessionResourceFileRow
	if err := namedSelectContext(ctx, d.sql, &rows, query, args); err != nil {
		return SessionResourceFilePage{}, err
	}
	entries, err := sessionResourceFilesFromSQLXRows(rows)
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

func buildSessionResourceFilesPageQuery(filesystem FilestoreFilesystem, params ListSessionResourceFilesPageParams) (string, map[string]any) {
	query := sessionResourceFileSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and kind <> 'archive'
			and deleted_at is null
			and (expires_at is null or expires_at > now())
	`
	args := map[string]any{
		"workspace_uuid": dbUUID(filesystem.WorkspaceUUID),
		"session_uuid":   dbUUID(filesystem.SessionUUID),
		"fetch_limit":    params.Limit + 1,
	}
	if params.Recursive {
		// 在 Go 中补齐分隔符，确保 /foo 不会误包含 /foobar。
		query += " and left(path, char_length(:directory_prefix)) = :directory_prefix"
		args["directory_prefix"] = filestoreDirectoryPrefix(params.DirectoryPath)
	} else {
		query += " and parent_path = :directory_path"
		args["directory_path"] = params.DirectoryPath
	}
	if params.Cursor != nil {
		query += " and (path, uuid) > (:cursor_path, :cursor_uuid)"
		args["cursor_path"] = params.Cursor.Path
		args["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	// 多取一条只用于判定 HasMore；返回页仍严格遵守请求的 Limit。
	query += " order by path asc, uuid asc limit :fetch_limit"
	return query, args
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
