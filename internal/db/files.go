package db

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type FileRecord struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	Filename            string
	MimeType            string
	SizeBytes           int64
	SHA256              string
	S3Bucket            string
	S3Key               string
	Downloadable        bool
	ScopeType           *string
	ScopeID             *string
	CreatedByAPIKeyUUID string
	CreatedAt           time.Time
}

type ListFilesPageParams struct {
	WorkspaceUUID string
	ScopeID       string
	AfterID       string
	BeforeID      string
	Limit         int
}

func (d *DB) WorkspaceStorageBytes(ctx context.Context, workspaceUUID string) (int64, error) {
	return d.GetWorkspaceStorageBytes(ctx, workspaceUUID)
}

func (d *DB) CreateFile(ctx context.Context, f FileRecord) error {
	return createFile(ctx, d.mapperDB, f)
}

func (d *DB) CreateFileIfWithinLimit(ctx context.Context, f FileRecord, workspaceStorageLimitBytes int64) error {
	return createFileIfWithinLimit(ctx, d.mapperDB, f, workspaceStorageLimitBytes)
}

func (d *DB) GetFile(ctx context.Context, workspaceUUID string, fileExternalID string) (FileRecord, error) {
	mapper := NewFileMapper(d.mapperDB)
	row, err := mapper.GetFile(ctx, workspaceUUID, fileExternalID)
	return fileRecordFromMapperRow(row, err)
}

func (d *DB) GetFileByUUID(ctx context.Context, workspaceUUID string, fileUUID string) (FileRecord, error) {
	mapper := NewFileMapper(d.mapperDB)
	row, err := mapper.GetFileByUUID(ctx, workspaceUUID, fileUUID)
	return fileRecordFromMapperRow(row, err)
}

func (d *DB) GetFileByUUIDInOrganization(ctx context.Context, organizationUUID string, fileUUID string) (FileRecord, error) {
	mapper := NewFileMapper(d.mapperDB)
	row, err := mapper.GetFileByUUIDInOrganization(
		ctx,
		organizationUUID,
		fileUUID,
	)
	return fileRecordFromMapperRow(row, err)
}

func (d *DB) ListFiles(ctx context.Context, workspaceUUID string, scopeID string) ([]FileRecord, error) {
	params := newFileMapperListParams(workspaceUUID, scopeID)
	mapper := NewFileMapper(d.mapperDB)
	var (
		rows []fileRecordRow
		err  error
	)
	if params.SessionScope {
		rows, err = mapper.ListSessionFiles(ctx, params)
	} else {
		rows, err = mapper.ListFiles(ctx, params)
	}
	return fileRecordsFromMapperRows(rows, err)
}

func (d *DB) ListFilesPage(ctx context.Context, params ListFilesPageParams) ([]FileRecord, bool, error) {
	return listFilesPage(ctx, d.mapperDB, params)
}

func (d *DB) SoftDeleteFile(ctx context.Context, workspaceUUID string, fileExternalID string) error {
	return softDeleteFile(ctx, d.mapperDB, workspaceUUID, fileExternalID)
}

// createFile 在同一个 Yourbatis 事务里写入文件记录并同步增加 workspace
// 存储用量，保证元数据与配额统计一致。
func createFile(ctx context.Context, database *yourbatis.DB, file FileRecord) error {
	return withFileCreateTransaction(ctx, database, file.WorkspaceUUID, func(executor yourbatis.Executor) error {
		if err := insertFileTx(ctx, executor, file); err != nil {
			return err
		}
		return applyWorkspaceStorageDeltaTx(ctx, executor, file.WorkspaceUUID, file.SizeBytes, 0, 0)
	})
}

// createFileIfWithinLimit 先尝试预扣本次写入会消耗的存储额度，只有在
// workspace 仍未超限时才真正插入文件记录。
func createFileIfWithinLimit(
	ctx context.Context,
	database *yourbatis.DB,
	file FileRecord,
	workspaceStorageLimitBytes int64,
) error {
	return withFileCreateTransaction(ctx, database, file.WorkspaceUUID, func(executor yourbatis.Executor) error {
		if err := applyWorkspaceStorageDeltaTx(
			ctx,
			executor,
			file.WorkspaceUUID,
			file.SizeBytes,
			0,
			workspaceStorageLimitBytes,
		); err != nil {
			return err
		}
		return insertFileTx(ctx, executor, file)
	})
}

func withFileCreateTransaction(
	ctx context.Context,
	database *yourbatis.DB,
	workspaceUUID string,
	fn func(yourbatis.Executor) error,
) error {
	return database.Transaction(ctx, func(executor yourbatis.Executor) error {
		storageMapper := NewWorkspaceStorageUsageMapper(executor)
		if err := storageMapper.LockWorkspace(ctx, workspaceUUID); err != nil {
			return err
		}
		return fn(executor)
	})
}

func insertFileTx(ctx context.Context, tx yourbatis.Executor, file FileRecord) error {
	mapper := NewFileMapper(tx)
	return mapper.InsertFile(ctx, fileMapperRecordParameters(file))
}

func listFilesPage(
	ctx context.Context,
	database yourbatis.Executor,
	params ListFilesPageParams,
) ([]FileRecord, bool, error) {
	params = normalizeListFilesPageParams(params)
	mapper := NewFileMapper(database)
	mapperParams := newFileMapperListParams(params.WorkspaceUUID, params.ScopeID)
	mapperParams.CursorExternalID = params.AfterID
	if mapperParams.CursorExternalID == "" {
		mapperParams.CursorExternalID = params.BeforeID
	}
	cursor, found, err := getFilePageCursor(ctx, mapper, mapperParams)
	if err != nil || !found {
		return nil, false, err
	}
	mapperParams.HasCursor = mapperParams.CursorExternalID != ""
	mapperParams.CursorUUID = cursor.UUID
	mapperParams.CursorCreatedAt = cursor.CreatedAt
	mapperParams.Limit = params.Limit + 1
	mapperParams.Before = params.BeforeID != ""

	var rows []fileRecordRow
	if mapperParams.SessionScope {
		rows, err = mapper.ListSessionFilesPage(ctx, mapperParams)
	} else {
		rows, err = mapper.ListFilesPage(ctx, mapperParams)
	}
	files, err := fileRecordsFromMapperRows(rows, err)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(files) > params.Limit
	if hasMore {
		files = files[:params.Limit]
	}
	if params.BeforeID != "" {
		slices.Reverse(files)
	}
	return files, hasMore, nil
}

func normalizeListFilesPageParams(params ListFilesPageParams) ListFilesPageParams {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}
	return params
}

func getFilePageCursor(
	ctx context.Context,
	mapper FileMapper,
	params fileMapperListParams,
) (filePageCursorRow, bool, error) {
	if params.CursorExternalID == "" {
		return filePageCursorRow{}, true, nil
	}
	if params.SessionScope {
		return mapper.FindSessionPageCursor(ctx, params)
	}
	return mapper.FindPageCursor(ctx, params)
}

func isSessionFilesScope(scopeID string) bool {
	return strings.HasPrefix(scopeID, "sesn_")
}

func softDeleteFile(
	ctx context.Context,
	database *yourbatis.DB,
	workspaceUUID string,
	fileExternalID string,
) error {
	return database.Transaction(ctx, func(executor yourbatis.Executor) error {
		storageMapper := NewWorkspaceStorageUsageMapper(executor)
		if err := storageMapper.LockWorkspace(ctx, workspaceUUID); err != nil {
			return err
		}
		mapper := NewFileMapper(executor)
		resolvedRow, err := mapper.GetFile(ctx, workspaceUUID, fileExternalID)
		resolved, err := fileRecordFromMapperRow(resolvedRow, err)
		if err != nil {
			return err
		}
		fileRow, err := mapper.GetFileForDelete(ctx, workspaceUUID, resolved.UUID)
		file, err := fileRecordFromMapperRow(fileRow, err)
		if err != nil {
			return err
		}
		referenced, err := mapper.HasActiveReference(ctx, workspaceUUID, resolved.UUID)
		if err != nil {
			return err
		}
		if referenced {
			return ErrFileInUse
		}
		if err := mapper.SoftDeleteFile(ctx, workspaceUUID, resolved.UUID); err != nil {
			return err
		}
		return applyWorkspaceStorageDeltaTx(ctx, executor, workspaceUUID, -file.SizeBytes, 0, 0)
	})
}
