package filestore

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

const (
	orphanCleanupDelay        = time.Hour
	defaultListDirectoryLimit = 100
	maxListDirectoryLimit     = 1000
)

type filestoreDatabase interface {
	GetFilestoreFilesystem(context.Context, int64, string) (db.FilestoreFilesystem, error)
	GetFilestoreEntry(context.Context, int64, int64, string) (db.FilestoreEntry, error)
	ListFilestoreEntriesPage(context.Context, db.ListFilestoreEntriesPageParams) (db.FilestoreEntryPage, error)
	MakeFilestoreDirectory(context.Context, db.MakeFilestoreDirectoryInput) (db.FilestoreEntry, error)
	PutFilestoreFile(context.Context, db.PutFilestoreFileInput) (db.FilestoreMutationResult, error)
	CopyFilestoreFile(context.Context, db.CopyFilestoreFileInput) (db.FilestoreMutationResult, error)
	MoveFilestoreFile(context.Context, db.MoveFilestoreFileInput) (db.FilestoreMutationResult, error)
	MoveFilestoreDirectory(context.Context, db.MoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error)
	RemoveFilestoreFile(context.Context, db.RemoveFilestoreEntryInput) (db.FilestoreMutationResult, error)
	RemoveFilestoreDirectory(context.Context, db.RemoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error)
	EnqueueFilestoreObjectCleanupJob(context.Context, db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error)
	AttachFilestoreObjectCleanupJobVersion(context.Context, int64, string, string, string) error
	CompleteFilestoreObjectCleanupJob(context.Context, int64) error
}

// Service 编排 Filestore 的鉴权上下文、元数据事务与对象存储操作。
// 数据库负责命名空间一致性，对象存储负责字节内容，两者通过持久化清理任务实现最终一致。
type Service struct {
	cfg   config.Config
	db    filestoreDatabase
	store storage.ObjectStore
	now   func() time.Time
}

type readFileResult struct {
	Body      io.ReadCloser
	Size      int64
	MediaType string
}

// NewService 创建 Filestore 业务服务。
func NewService(cfg config.Config, database filestoreDatabase, store storage.ObjectStore) *Service {
	return &Service{cfg: cfg, db: database, store: store, now: time.Now}
}

// ListDirectory 按路径与内部 ID 的稳定顺序列出目录，使用键集游标避免 offset 分页漂移。
func (s *Service) ListDirectory(ctx context.Context, principal Principal, request listDirectoryRequest) (listDirectoryResponse, *apiError) {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, true); apiErr != nil {
		return listDirectoryResponse{}, apiErr
	}
	limit := int64(request.Limit)
	if limit == 0 {
		limit = defaultListDirectoryLimit
	}
	if limit < 1 || limit > maxListDirectoryLimit {
		return listDirectoryResponse{}, invalidArgument("limit must be between 1 and 1000")
	}
	cursor, err := decodeDirectoryCursor(request.Cursor, request.FilesystemID, request.Path, request.Recursive)
	if err != nil {
		return listDirectoryResponse{}, invalidArgument(err.Error())
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return listDirectoryResponse{}, apiErr
	}
	params := db.ListFilestoreEntriesPageParams{
		WorkspaceID:   principal.WorkspaceID,
		FilesystemID:  filesystem.ID,
		DirectoryPath: request.Path,
		Recursive:     request.Recursive,
		Limit:         int(limit),
	}
	if request.Cursor != "" {
		// Path 是主排序键，ID 在路径相同的边界情形下提供稳定的决胜键。
		params.Cursor = &db.FilestoreEntryPageCursor{Path: cursor.LastPath, ID: cursor.LastID}
	}
	page, err := s.db.ListFilestoreEntriesPage(ctx, params)
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
			LastID:       last.ID,
		})
		if err != nil {
			return listDirectoryResponse{}, internalError("encode directory cursor", err)
		}
	}
	return response, nil
}

// MakeDirectory 创建目录；MakeParents 为真时在同一事务内补齐整条父目录链。
func (s *Service) MakeDirectory(ctx context.Context, principal Principal, request makeDirectoryRequest) (directoryResponse, *apiError) {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, true); apiErr != nil {
		return directoryResponse{}, apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return directoryResponse{}, apiErr
	}
	entry, err := s.db.MakeFilestoreDirectory(ctx, db.MakeFilestoreDirectoryInput{
		WorkspaceID:  principal.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         request.Path,
		MakeParents:  request.MakeParents,
		Now:          s.now().UTC(),
	})
	if err != nil {
		return directoryResponse{}, mapDatabaseError("make directory", err)
	}
	return directoryResponse{Directory: directoryPayloadFromEntry(entry, filesystem.ExternalID)}, nil
}

// RemoveDirectory 删除目录。重复删除视为成功，以满足文件系统客户端的幂等预期。
func (s *Service) RemoveDirectory(ctx context.Context, principal Principal, request removeDirectoryRequest) *apiError {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, false); apiErr != nil {
		return apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return apiErr
	}
	_, err := s.db.RemoveFilestoreDirectory(ctx, db.RemoveFilestoreDirectoryInput{
		WorkspaceID:  principal.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         request.Path,
		Recursive:    request.Recursive,
		Now:          s.now().UTC(),
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	return mapDatabaseErrorOrNil("remove directory", err)
}

// CreateFile 流式上传文件并写入元数据。上传前先登记延迟清理哨兵，
// 使进程在“对象已写入、数据库尚未提交”之间崩溃时仍能回收孤儿对象。
func (s *Service) CreateFile(ctx context.Context, principal Principal, params createFileParams, body io.Reader) (fileResponse, *apiError) {
	if apiErr := validateCreateFileParams(params); apiErr != nil {
		return fileResponse{}, apiErr
	}
	if body == nil {
		return fileResponse{}, invalidArgument("file body is required")
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, params.FilesystemID)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	if apiErr := s.requireParentDirectory(ctx, principal.WorkspaceID, filesystem.ID, params.Path); apiErr != nil {
		return fileResponse{}, apiErr
	}
	now := s.now().UTC()
	expiresAt, apiErr := filestoreExpiry(now, int64(params.TTLSeconds))
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	metadata, authorization, apiErr := marshalFileMetadata(params.Metadata, params.Authorization)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	md5Hash := md5.New()
	sha256Hash := sha256.New()
	// 限额、双摘要与上传共用一条流：不预读整文件，也不重复遍历内容。
	uploadReader := limitedUploadReader(body, s.cfg.Storage.MaxFileBytes)
	hashedReader := io.TeeReader(uploadReader, io.MultiWriter(md5Hash, sha256Hash))
	mediaType := normalizeMediaType(params.MediaType)
	var upload storage.UploadResult
	staged, apiErr := s.stageFilestoreObject(ctx, principal, filesystem, now, func(objectKey string) (objectWriteResult, *apiError) {
		var err error
		upload, err = s.store.Upload(ctx, objectKey, hashedReader, storage.UploadOptions{Size: -1, ContentType: mediaType})
		result := objectWriteResult{ETag: upload.ETag, VersionID: upload.VersionID}
		if err != nil {
			return result, mapBlobstoreError("upload file", err)
		}
		if s.cfg.Storage.MaxFileBytes > 0 && upload.Size > s.cfg.Storage.MaxFileBytes {
			return result, &apiError{Status: http.StatusRequestEntityTooLarge, Code: "resource_exhausted", Message: "File exceeds maximum size"}
		}
		return result, nil
	})
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	result, err := s.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceID:  principal.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         params.Path,
		Blob: db.FilestoreFileBlob{
			SizeBytes:             upload.Size,
			MediaType:             mediaType,
			DetectedMimeType:      mediaType,
			Metadata:              metadata,
			AuthorizationMetadata: authorization,
			Tags:                  append([]string(nil), params.Tags...),
			Downloadable:          fileDownloadable(params.Authorization),
			MD5:                   hex.EncodeToString(md5Hash.Sum(nil)),
			SHA256:                hex.EncodeToString(sha256Hash.Sum(nil)),
			S3Bucket:              s.store.Name(),
			S3Key:                 staged.Key,
			S3ETag:                upload.ETag,
			S3VersionID:           upload.VersionID,
			ExpiresAt:             expiresAt,
		},
		OverwriteExisting:          params.OverwriteExisting,
		OrphanCleanupJobExternalID: staged.CleanupJob.ExternalID,
		WorkspaceStorageLimitBytes: s.cfg.Storage.WorkspaceLimitBytes,
		Now:                        now,
	})
	if err != nil {
		// Commit 报错时事务结果可能未知：保留延迟清理哨兵，而不立即删除可能已被有效记录引用的对象。
		// 若提交其实成功，事务已原子取消该哨兵；否则后台清理会在宽限期后回收对象。
		return fileResponse{}, mapDatabaseError("create file", err)
	}
	payload, err := filePayloadFromEntry(result.Entry, filesystem.ExternalID)
	if err != nil {
		return fileResponse{}, internalError("encode file", err)
	}
	return fileResponse{File: payload}, nil
}

// CopyFile 在对象存储端复制字节内容，再以乐观校验将新对象绑定到目标路径。
func (s *Service) CopyFile(ctx context.Context, principal Principal, request copyMoveFileRequest) (fileResponse, *apiError) {
	if apiErr := validateFileTransferRequest(request); apiErr != nil {
		return fileResponse{}, apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	source, err := s.db.GetFilestoreEntry(ctx, principal.WorkspaceID, filesystem.ID, request.Source)
	if err != nil {
		return fileResponse{}, mapDatabaseError("read copy source", err)
	}
	if source.Kind != db.FilestoreEntryKindFile || source.S3Key == nil {
		return fileResponse{}, failedPrecondition("source is not a file")
	}
	if apiErr := s.requireParentDirectory(ctx, principal.WorkspaceID, filesystem.ID, request.Destination); apiErr != nil {
		return fileResponse{}, apiErr
	}
	now := s.now().UTC()
	var copyResult storage.CopyResult
	staged, apiErr := s.stageFilestoreObject(ctx, principal, filesystem, now, func(destinationKey string) (objectWriteResult, *apiError) {
		var err error
		copyResult, err = s.store.Copy(ctx, *source.S3Key, destinationKey)
		result := objectWriteResult{ETag: copyResult.ETag, VersionID: copyResult.VersionID}
		if err != nil {
			return result, mapBlobstoreError("copy file", err)
		}
		return result, nil
	})
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	result, err := s.db.CopyFilestoreFile(ctx, db.CopyFilestoreFileInput{
		WorkspaceID:                principal.WorkspaceID,
		FilesystemID:               filesystem.ID,
		SourcePath:                 request.Source,
		DestinationPath:            request.Destination,
		ExpectedSourceS3Key:        *source.S3Key,
		ExpectedSourceS3VersionID:  stringValue(source.S3VersionID),
		DestinationS3Bucket:        s.store.Name(),
		DestinationS3Key:           staged.Key,
		DestinationS3ETag:          copyResult.ETag,
		DestinationS3VersionID:     copyResult.VersionID,
		OverwriteExisting:          request.OverwriteExisting,
		OrphanCleanupJobExternalID: staged.CleanupJob.ExternalID,
		WorkspaceStorageLimitBytes: s.cfg.Storage.WorkspaceLimitBytes,
		Now:                        now,
	})
	if err != nil {
		// 与 CreateFile 相同，提交结果未知时由延迟哨兵裁决，避免误删已提交的新副本。
		return fileResponse{}, mapDatabaseError("copy file", err)
	}
	payload, err := filePayloadFromEntry(result.Entry, filesystem.ExternalID)
	if err != nil {
		return fileResponse{}, internalError("encode copied file", err)
	}
	return fileResponse{File: payload}, nil
}

// MoveFile 只移动命名空间中的路径；对象键保持不变，因此无需复制文件内容。
func (s *Service) MoveFile(ctx context.Context, principal Principal, request copyMoveFileRequest) (fileResponse, *apiError) {
	if apiErr := validateFileTransferRequest(request); apiErr != nil {
		return fileResponse{}, apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	result, err := s.db.MoveFilestoreFile(ctx, db.MoveFilestoreFileInput{
		WorkspaceID:       principal.WorkspaceID,
		FilesystemID:      filesystem.ID,
		SourcePath:        request.Source,
		DestinationPath:   request.Destination,
		OverwriteExisting: request.OverwriteExisting,
		Now:               s.now().UTC(),
	})
	if err != nil {
		return fileResponse{}, mapDatabaseError("move file", err)
	}
	payload, err := filePayloadFromEntry(result.Entry, filesystem.ExternalID)
	if err != nil {
		return fileResponse{}, internalError("encode moved file", err)
	}
	return fileResponse{File: payload}, nil
}

// MoveDirectory 在单个数据库事务内重写整棵子树的路径，不搬运底层对象。
func (s *Service) MoveDirectory(ctx context.Context, principal Principal, request moveDirectoryRequest) (directoryResponse, *apiError) {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Source, false); apiErr != nil {
		return directoryResponse{}, apiErr
	}
	if err := validateFilestorePath(request.Destination, false); err != nil {
		return directoryResponse{}, invalidArgument("destination: " + err.Error())
	}
	if isDescendant(request.Destination, request.Source) {
		return directoryResponse{}, invalidArgument("destination must not be inside source")
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return directoryResponse{}, apiErr
	}
	result, err := s.db.MoveFilestoreDirectory(ctx, db.MoveFilestoreDirectoryInput{
		WorkspaceID:     principal.WorkspaceID,
		FilesystemID:    filesystem.ID,
		SourcePath:      request.Source,
		DestinationPath: request.Destination,
		Now:             s.now().UTC(),
	})
	if err != nil {
		return directoryResponse{}, mapDatabaseError("move directory", err)
	}
	return directoryResponse{Directory: directoryPayloadFromEntry(result.Entry, filesystem.ExternalID)}, nil
}

// ReadFile 打开完整文件或指定字节区间；元数据存在而对象缺失视为服务端一致性故障。
func (s *Service) ReadFile(ctx context.Context, principal Principal, request readFileRequest) (readFileResult, *apiError) {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, false); apiErr != nil {
		return readFileResult{}, apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	entry, err := s.db.GetFilestoreEntry(ctx, principal.WorkspaceID, filesystem.ID, request.Path)
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
	object, err := s.store.Open(ctx, *entry.S3Key, objectRange)
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

// RemoveFile 软删除元数据并登记对象清理任务；重复删除按幂等成功处理。
func (s *Service) RemoveFile(ctx context.Context, principal Principal, request pathRequest) *apiError {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, false); apiErr != nil {
		return apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return apiErr
	}
	_, err := s.db.RemoveFilestoreFile(ctx, db.RemoveFilestoreEntryInput{
		WorkspaceID:  principal.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         request.Path,
		Now:          s.now().UTC(),
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	return mapDatabaseErrorOrNil("remove file", err)
}

// ReadMetadata 返回文件或目录的协议元数据，不读取对象内容。
func (s *Service) ReadMetadata(ctx context.Context, principal Principal, request pathRequest) (entryPayload, *apiError) {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Path, true); apiErr != nil {
		return entryPayload{}, apiErr
	}
	filesystem, apiErr := s.resolveFilesystem(ctx, principal, request.FilesystemID)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	entry, err := s.db.GetFilestoreEntry(ctx, principal.WorkspaceID, filesystem.ID, request.Path)
	if err != nil {
		return entryPayload{}, mapDatabaseError("read metadata", err)
	}
	payload, err := payloadFromEntry(entry, filesystem.ExternalID)
	if err != nil {
		return entryPayload{}, internalError("encode metadata", err)
	}
	return payload, nil
}

func (s *Service) resolveFilesystem(ctx context.Context, principal Principal, filesystemID string) (db.FilestoreFilesystem, *apiError) {
	if principal.WorkspaceID <= 0 {
		return db.FilestoreFilesystem{}, &apiError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "Invalid principal"}
	}
	// 数据库回查已把 claim 解析成规范的 external ID 与 UUID。请求可任选其一，
	// 但不能借此访问同工作区内的其他 filesystem。
	if principal.FilesystemInternalID <= 0 ||
		(filesystemID != principal.FilesystemExternalID && !strings.EqualFold(filesystemID, principal.FilesystemUUID)) {
		return db.FilestoreFilesystem{}, permissionDenied("Filestore token does not grant access to this filesystem")
	}
	filesystem, err := s.db.GetFilestoreFilesystem(ctx, principal.WorkspaceID, filesystemID)
	if err != nil {
		return db.FilestoreFilesystem{}, mapDatabaseError("resolve filesystem", err)
	}
	if filesystem.ID != principal.FilesystemInternalID {
		return db.FilestoreFilesystem{}, permissionDenied("Filestore token does not grant access to this filesystem")
	}
	return filesystem, nil
}

func (s *Service) requireParentDirectory(ctx context.Context, workspaceID, filesystemID int64, entryPath string) *apiError {
	parent := parentPath(entryPath)
	entry, err := s.db.GetFilestoreEntry(ctx, workspaceID, filesystemID, parent)
	if errors.Is(err, db.ErrNotFound) {
		return failedPrecondition("parent directory does not exist")
	}
	if err != nil {
		return mapDatabaseError("read parent directory", err)
	}
	if entry.Kind != db.FilestoreEntryKindDirectory {
		return failedPrecondition("parent path is not a directory")
	}
	return nil
}

func (s *Service) enqueueOrphanCleanup(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryExternalID string,
	key string,
	now time.Time,
) (db.FilestoreObjectCleanupJob, *apiError) {
	// 先写哨兵、后写对象：任何后续失败都有一条持久化补偿路径。
	job, err := s.db.EnqueueFilestoreObjectCleanupJob(ctx, db.EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceID:     principal.WorkspaceID,
		FilesystemID:    filesystem.ID,
		EntryExternalID: entryExternalID,
		Bucket:          s.store.Name(),
		Key:             key,
		Reason:          "orphan_guard",
		RunAfter:        now.Add(orphanCleanupDelay),
	})
	if err != nil {
		return db.FilestoreObjectCleanupJob{}, internalError("prepare object cleanup", err)
	}
	return job, nil
}

type objectWriteResult struct {
	ETag      string
	VersionID string
}

type stagedFilestoreObject struct {
	Key        string
	CleanupJob db.FilestoreObjectCleanupJob
}

// stageFilestoreObject 统一保护“先写对象、后提交元数据”的非原子窗口。
// 写入或版本登记失败时立即尝试回收；数据库提交结果未知时则由调用方保留哨兵等待后台裁决。
func (s *Service) stageFilestoreObject(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	now time.Time,
	write func(string) (objectWriteResult, *apiError),
) (stagedFilestoreObject, *apiError) {
	blobUUID := uuid.NewString()
	key := filestoreObjectKey(principal.WorkspaceUUID, filesystem.UUID, blobUUID)
	cleanupJob, apiErr := s.enqueueOrphanCleanup(ctx, principal, filesystem, blobUUID, key, now)
	if apiErr != nil {
		return stagedFilestoreObject{}, apiErr
	}
	result, apiErr := write(key)
	if apiErr != nil {
		s.discardOrphan(ctx, cleanupJob, key, result.VersionID)
		return stagedFilestoreObject{}, apiErr
	}
	if apiErr := s.attachOrphanVersion(ctx, principal.WorkspaceID, cleanupJob, result.ETag, result.VersionID); apiErr != nil {
		s.discardOrphan(ctx, cleanupJob, key, result.VersionID)
		return stagedFilestoreObject{}, apiErr
	}
	return stagedFilestoreObject{Key: key, CleanupJob: cleanupJob}, nil
}

func (s *Service) discardOrphan(ctx context.Context, job db.FilestoreObjectCleanupJob, key, versionID string) {
	// 客户端取消请求后仍给补偿动作一个短暂独立窗口；删除失败则保留任务交由后台重试。
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	deleteOptions := storage.DeleteOptions{VersionID: versionID, AllVersions: versionID == ""}
	if err := s.store.Delete(cleanupCtx, key, deleteOptions); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return
	}
	_ = s.db.CompleteFilestoreObjectCleanupJob(cleanupCtx, job.ID)
}

func (s *Service) attachOrphanVersion(
	ctx context.Context,
	workspaceID int64,
	job db.FilestoreObjectCleanupJob,
	etag string,
	versionID string,
) *apiError {
	if err := s.db.AttachFilestoreObjectCleanupJobVersion(ctx, workspaceID, job.ExternalID, etag, versionID); err != nil {
		return internalError("record uploaded object version", err)
	}
	return nil
}

func validateFilesystemAndPath(filesystemID, value string, allowRoot bool) *apiError {
	if err := validateFilesystemID(filesystemID); err != nil {
		return invalidArgument(err.Error())
	}
	if err := validateFilestorePath(value, allowRoot); err != nil {
		return invalidArgument(err.Error())
	}
	return nil
}

func validateCreateFileParams(params createFileParams) *apiError {
	if apiErr := validateFilesystemAndPath(params.FilesystemID, params.Path, false); apiErr != nil {
		return apiErr
	}
	if err := validateMediaType(params.MediaType); err != nil {
		return invalidArgument(err.Error())
	}
	if err := validateAuthorizationMetadata(params.Authorization); err != nil {
		return invalidArgument(err.Error())
	}
	for _, tag := range params.Tags {
		if strings.TrimSpace(tag) == "" {
			return invalidArgument("tags must not contain empty values")
		}
	}
	return nil
}

func validateFileTransferRequest(request copyMoveFileRequest) *apiError {
	if apiErr := validateFilesystemAndPath(request.FilesystemID, request.Source, false); apiErr != nil {
		return apiErr
	}
	if err := validateFilestorePath(request.Destination, false); err != nil {
		return invalidArgument("destination: " + err.Error())
	}
	return nil
}

func marshalFileMetadata(metadata map[string]any, authorization *authorizationMetadata) (json.RawMessage, json.RawMessage, *apiError) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, invalidArgument("metadata is not valid JSON")
	}
	authorizationJSON := json.RawMessage(`{}`)
	if authorization != nil {
		authorizationJSON, err = json.Marshal(authorization)
		if err != nil {
			return nil, nil, invalidArgument("authorizationMetadata is not valid JSON")
		}
	}
	return metadataJSON, authorizationJSON, nil
}

func fileDownloadable(metadata *authorizationMetadata) bool {
	if metadata == nil {
		return true
	}
	return metadata.Downloadable
}

func filestoreExpiry(now time.Time, ttlSeconds int64) (*time.Time, *apiError) {
	if ttlSeconds == 0 {
		return nil, nil
	}
	if ttlSeconds < 0 {
		return nil, invalidArgument("ttlSeconds must not be negative")
	}
	maxUnix := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Unix()
	if ttlSeconds > maxUnix-now.Unix() {
		return nil, invalidArgument("ttlSeconds is too large")
	}
	expiresAt := time.Unix(now.Unix()+ttlSeconds, int64(now.Nanosecond())).UTC()
	return &expiresAt, nil
}

func limitedUploadReader(body io.Reader, maxBytes int64) io.Reader {
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return body
	}
	// 多读 1 字节才能区分“恰好达到上限”和“确实超限”。
	return io.LimitReader(body, maxBytes+1)
}

func filestoreObjectKey(workspaceUUID, filesystemUUID, blobUUID string) string {
	return fmt.Sprintf("workspaces/%s/filestores/%s/blobs/%s", workspaceUUID, filesystemUUID, blobUUID)
}

func resolveReadRange(requestRange *readFileRange, fileSize int64) (*storage.ByteRange, int64, *apiError) {
	if requestRange == nil {
		return nil, fileSize, nil
	}
	offset := int64(requestRange.Offset)
	length := int64(requestRange.Length)
	if offset < 0 || length < -1 {
		return nil, 0, invalidRange("range offset and length are invalid")
	}
	if offset > fileSize {
		return nil, 0, invalidRange("range offset exceeds file size")
	}
	if length == 0 || offset == fileSize {
		return nil, 0, nil
	}
	if length == -1 {
		return &storage.ByteRange{Offset: offset, Length: -1}, fileSize - offset, nil
	}
	remaining := fileSize - offset
	if length > remaining {
		// 协议允许请求越过文件尾，实际读取长度收敛到剩余字节数。
		length = remaining
	}
	return &storage.ByteRange{Offset: offset, Length: length}, length, nil
}

func payloadFromEntry(entry db.FilestoreEntry, filesystemExternalID string) (entryPayload, error) {
	if entry.Kind == db.FilestoreEntryKindDirectory {
		directory := directoryPayloadFromEntry(entry, filesystemExternalID)
		return entryPayload{Directory: &directory}, nil
	}
	if entry.Kind != db.FilestoreEntryKindFile {
		return entryPayload{}, fmt.Errorf("unsupported filestore entry kind %q", entry.Kind)
	}
	file, err := filePayloadFromEntry(entry, filesystemExternalID)
	if err != nil {
		return entryPayload{}, err
	}
	return entryPayload{File: &file}, nil
}

func filePayloadFromEntry(entry db.FilestoreEntry, filesystemExternalID string) (filesystemFilePayload, error) {
	if entry.Kind != db.FilestoreEntryKindFile || entry.SizeBytes == nil {
		return filesystemFilePayload{}, errors.New("filestore entry is not a complete file")
	}
	metadata := map[string]any{}
	if len(entry.Metadata) != 0 && string(entry.Metadata) != "null" {
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return filesystemFilePayload{}, fmt.Errorf("decode file metadata: %w", err)
		}
	}
	payload := filePayload{
		UUID:             entry.UUID,
		CreatedAt:        formatTimestamp(entry.CreatedAt),
		Size:             protoInt64(*entry.SizeBytes),
		MediaType:        stringValue(entry.MediaType),
		Metadata:         metadata,
		MD5:              stringValue(entry.MD5),
		EntryTaggedID:    entry.ExternalID,
		DetectedMimeType: stringValue(entry.DetectedMimeType),
		Downloadable:     entry.Downloadable,
		Tags:             append([]string(nil), entry.Tags...),
		FilesystemID:     filesystemExternalID,
	}
	if entry.ExpiresAt != nil {
		payload.ExpiresAt = formatTimestamp(*entry.ExpiresAt)
	}
	return filesystemFilePayload{File: payload, FilesystemID: filesystemExternalID, Path: entry.Path}, nil
}

func directoryPayloadFromEntry(entry db.FilestoreEntry, filesystemExternalID string) directoryPayload {
	return directoryPayload{
		FilesystemID: filesystemExternalID,
		Path:         entry.Path,
		CreatedAt:    formatTimestamp(entry.CreatedAt),
	}
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapDatabaseErrorOrNil(operation string, err error) *apiError {
	if err == nil {
		return nil
	}
	return mapDatabaseError(operation, err)
}

func mapDatabaseError(operation string, err error) *apiError {
	switch {
	case errors.Is(err, db.ErrFilestoreParentMissing):
		return failedPreconditionWithCause("parent directory does not exist", err)
	case errors.Is(err, db.ErrNotFound):
		return notFound("resource does not exist")
	case errors.Is(err, db.ErrFilestorePathExists), errors.Is(err, db.ErrDuplicate):
		return &apiError{Status: http.StatusConflict, Code: "already_exists", Message: "destination already exists", Cause: err}
	case errors.Is(err, db.ErrFilestoreNotFile):
		return failedPreconditionWithCause("path is not a file", err)
	case errors.Is(err, db.ErrFilestoreNotDirectory):
		return failedPreconditionWithCause("path is not a directory", err)
	case errors.Is(err, db.ErrFilestoreDirectoryNotEmpty):
		return failedPreconditionWithCause("directory is not empty", err)
	case errors.Is(err, db.ErrFilestoreInvalidMove), errors.Is(err, db.ErrPreconditionFailed):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_argument", Message: "request violates a filestore precondition", Cause: err}
	case errors.Is(err, db.ErrStorageLimitExceeded):
		return &apiError{Status: http.StatusForbidden, Code: "resource_exhausted", Message: "Workspace storage limit exceeded", Cause: err}
	case errors.Is(err, db.ErrVersionConflict):
		return &apiError{Status: http.StatusConflict, Code: "conflict", Message: "resource changed concurrently", Cause: err}
	default:
		return internalError(operation, err)
	}
}

func mapBlobstoreError(operation string, err error) *apiError {
	switch {
	case errors.Is(err, storage.ErrInvalidRange):
		return invalidRange("requested range is invalid")
	case errors.Is(err, storage.ErrNotFound):
		return notFound("file does not exist")
	default:
		return &apiError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "Object storage is unavailable", Cause: fmt.Errorf("%s: %w", operation, err)}
	}
}

func notFound(message string) *apiError {
	return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: message, Cause: errNotFound}
}

func failedPrecondition(message string) *apiError {
	return failedPreconditionWithCause(message, errFailedPrecondition)
}

func failedPreconditionWithCause(message string, cause error) *apiError {
	return &apiError{Status: http.StatusConflict, Code: "failed_precondition", Message: message, Cause: cause}
}

func invalidRange(message string) *apiError {
	return &apiError{Status: http.StatusRequestedRangeNotSatisfiable, Code: "invalid_argument", Message: message}
}

func internalError(operation string, err error) *apiError {
	return &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "Internal server error", Cause: fmt.Errorf("%s: %w", operation, err)}
}
