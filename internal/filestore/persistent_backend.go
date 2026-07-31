package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type persistentPathBackend struct {
	db    filestoreDatabase
	store storage.ObjectStore
}

func (b *persistentPathBackend) listDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request listDirectoryRequest,
	cursor directoryCursor,
	limit int,
) (listDirectoryResponse, *apiError) {
	params := db.ListFilestoreEntriesPageParams{
		WorkspaceUUID:  principal.WorkspaceUUID,
		FilesystemUUID: filesystem.UUID,
		DirectoryPath:  request.Path,
		Recursive:      request.Recursive,
		Limit:          limit,
	}
	if request.Cursor != "" {
		// Path 是主排序键，UUID 在路径相同的边界情形下提供稳定的决胜键。
		params.Cursor = &db.FilestoreEntryPageCursor{Path: cursor.LastPath, UUID: cursor.LastUUID}
	}
	page, err := b.db.ListFilestoreEntriesPage(ctx, params)
	if err != nil {
		return listDirectoryResponse{}, mapDatabaseError("list directory", err)
	}
	entries := page.Entries
	response := listDirectoryResponse{Entries: make([]entryPayload, 0, len(entries))}
	for _, entry := range entries {
		payload, err := payloadFromEntry(entry, filesystem.ExternalID)
		if err != nil {
			return listDirectoryResponse{}, internalError("encode directory entry", err)
		}
		response.Entries = append(response.Entries, payload)
	}
	if page.HasMore && len(entries) != 0 {
		// 只在确有下一页时签发游标；最后一页返回空 cursor，rclone 据此停止翻页。
		last := entries[len(entries)-1]
		response.Cursor, err = encodeDirectoryCursor(directoryCursor{
			FilesystemID: request.FilesystemID,
			Path:         request.Path,
			Recursive:    request.Recursive,
			LastPath:     last.Path,
			LastUUID:     last.UUID,
		})
		if err != nil {
			return listDirectoryResponse{}, internalError("encode directory cursor", err)
		}
	}
	return response, nil
}

func (b *persistentPathBackend) readFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request readFileRequest,
) (readFileResult, *apiError) {
	entry, err := b.db.GetFilestoreEntry(ctx, principal.WorkspaceUUID, filesystem.UUID, request.Path)
	if err != nil {
		return readFileResult{}, mapDatabaseError("read file metadata", err)
	}
	if entry.Kind != db.FilestoreEntryKindFile || entry.S3Key == nil || entry.SizeBytes == nil {
		return readFileResult{}, failedPrecondition("path is not a file")
	}
	objectRange, responseSize, apiErr := resolveReadRange(request.Range, *entry.SizeBytes)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	mediaType := stringValue(entry.MediaType)
	if responseSize == 0 {
		// 空区间无需访问 S3；仍返回可关闭的空流，使 Handler 的生命周期保持统一。
		return readFileResult{Body: io.NopCloser(bytes.NewReader(nil)), MediaType: mediaType}, nil
	}
	object, err := b.store.Open(ctx, *entry.S3Key, objectRange)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return readFileResult{}, internalError("read file object", errors.New("object metadata exists but blob is missing"))
		}
		return readFileResult{}, mapBlobstoreError("read file", err)
	}
	// 数据库元数据与已解析区间共同决定协议层应返回的精确字节数。
	// S3 响应可能没有 Content-Length（Object.Size 为 -1），不能让传输语义取决于该可选响应头。
	return readFileResult{Body: object.Body, Size: responseSize, MediaType: mediaType}, nil
}

func (b *persistentPathBackend) readMetadata(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (entryPayload, *apiError) {
	entry, err := b.db.GetFilestoreEntry(ctx, principal.WorkspaceUUID, filesystem.UUID, entryPath)
	if err != nil {
		return entryPayload{}, mapDatabaseError("read metadata", err)
	}
	payload, err := payloadFromEntry(entry, filesystem.ExternalID)
	if err != nil {
		return entryPayload{}, internalError("encode metadata", err)
	}
	return payload, nil
}
