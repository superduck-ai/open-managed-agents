package db

import "context"

const (
	defaultSessionNamespaceNodesPageLimit = 100
	maxSessionNamespaceNodesPageLimit     = 1000
)

// GetSessionNamespaceNode 在工作区与文件系统双重边界内读取一个有效节点。
// 根目录不落表，由文件系统记录即时投影为虚拟目录。
func (d *DB) GetSessionNamespaceNode(ctx context.Context, workspaceID, filesystemID int64, entryPath string) (SessionNamespaceNode, error) {
	if err := validateFilestorePath(entryPath); err != nil {
		return SessionNamespaceNode{}, err
	}
	filesystem, err := getFilestoreFilesystemByIDSQLX(ctx, d.sql, workspaceID, filesystemID)
	if err != nil {
		return SessionNamespaceNode{}, err
	}
	if entryPath == "/" {
		return virtualFilestoreRoot(filesystem), nil
	}
	return getActiveSessionNamespaceNodeSQLX(ctx, d.sql, filesystem, entryPath)
}

// ListSessionNamespaceNodesPage 以 (path, id) 为稳定排序键执行键集分页。
// 过期或软删除节点不会出现在结果中。
func (d *DB) ListSessionNamespaceNodesPage(ctx context.Context, params ListSessionNamespaceNodesPageParams) (SessionNamespaceNodePage, error) {
	if err := validateFilestorePath(params.DirectoryPath); err != nil {
		return SessionNamespaceNodePage{}, err
	}
	params.Limit = normalizeSessionNamespaceNodesPageLimit(params.Limit)
	filesystem, err := d.resolveFilestoreDirectoryForRead(ctx, params.WorkspaceID, params.FilesystemID, params.DirectoryPath)
	if err != nil {
		return SessionNamespaceNodePage{}, err
	}
	query, args := buildSessionNamespaceNodesPageQuery(filesystem, params)
	var rows []sessionNamespaceNodeRow
	if err := namedSelectContext(ctx, d.sql, &rows, query, args); err != nil {
		return SessionNamespaceNodePage{}, err
	}
	entries, err := sessionNamespaceNodesFromSQLXRows(rows)
	if err != nil {
		return SessionNamespaceNodePage{}, err
	}
	return newSessionNamespaceNodePage(entries, params.Limit), nil
}

func normalizeSessionNamespaceNodesPageLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultSessionNamespaceNodesPageLimit
	case limit > maxSessionNamespaceNodesPageLimit:
		return maxSessionNamespaceNodesPageLimit
	default:
		return limit
	}
}

func buildSessionNamespaceNodesPageQuery(filesystem FilestoreFilesystem, params ListSessionNamespaceNodesPageParams) (string, map[string]any) {
	query := sessionNamespaceNodeSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and kind <> 'archive'
			and deleted_at is null
			and (expires_at is null or expires_at > now())
	`
	args := map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"fetch_limit":     params.Limit + 1,
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
		query += " and (path, id) > (:cursor_path, :cursor_id)"
		args["cursor_path"] = params.Cursor.Path
		args["cursor_id"] = params.Cursor.ID
	}
	// 多取一条只用于判定 HasMore；返回页仍严格遵守请求的 Limit。
	query += " order by path asc, id asc limit :fetch_limit"
	return query, args
}

func filestoreDirectoryPrefix(directoryPath string) string {
	if directoryPath == "/" {
		return directoryPath
	}
	return directoryPath + "/"
}

func newSessionNamespaceNodePage(entries []SessionNamespaceNode, limit int) SessionNamespaceNodePage {
	page := SessionNamespaceNodePage{Entries: entries, HasMore: len(entries) > limit}
	if page.HasMore {
		page.Entries = entries[:limit]
	}
	return page
}
