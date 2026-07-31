package filestore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"golang.org/x/sync/singleflight"
)

const (
	skillNamespacePath                     = "/skills"
	maxSkillArchiveBytes            int64  = 8 * 1024 * 1024
	maxSkillUncompressedBytes       uint64 = 500 * 1024 * 1024
	defaultSkillArchiveCacheEntries        = 20
	defaultSkillArchiveLoadTimeout         = 30 * time.Second
)

type skillArchiveNode struct {
	path      string
	directory bool
	size      int64
	mediaType string
	file      *zip.File
}

type loadedSkillArchive struct {
	data  []byte
	nodes map[string]skillArchiveNode
}

type skillArchivePathBackend struct {
	db           filestoreDatabase
	store        storage.ObjectStore
	cache        *lru.Cache[string, *loadedSkillArchive]
	archiveLoads singleflight.Group
	loadTimeout  time.Duration
}

func newSkillArchiveCache(maxEntries int) *lru.Cache[string, *loadedSkillArchive] {
	cache, err := lru.New[string, *loadedSkillArchive](maxEntries)
	if err != nil {
		panic(fmt.Sprintf("create skill archive cache: %v", err))
	}
	return cache
}

func (b *skillArchivePathBackend) namespaceRoot() string {
	return skillNamespacePath
}

func (b *skillArchivePathBackend) containsPath(value string) bool {
	return value == skillNamespacePath || strings.HasPrefix(value, skillNamespacePath+"/")
}

func (b *skillArchivePathBackend) matchesRead(operation readOperation, value string) bool {
	if strings.HasPrefix(value, skillNamespacePath+"/") {
		return true
	}
	// /skills 本身是持久化目录：只有目录列举需要切换到虚拟 archive 视图，
	// metadata 和 file read 仍按普通 entry 语义处理。
	return operation == readOperationListDirectory && value == skillNamespacePath
}

func (b *skillArchivePathBackend) listDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request listDirectoryRequest,
	cursor directoryCursor,
	limit int,
) (listDirectoryResponse, *apiError) {
	archiveEntries, err := b.db.ListFilestoreSkillArchiveEntries(ctx, principal.WorkspaceUUID, filesystem.UUID)
	if err != nil {
		return listDirectoryResponse{}, mapDatabaseError("list skill archive entries", err)
	}
	nodes := make([]skillArchiveNode, 0)
	directoryExists := request.Path == skillNamespacePath
	for _, archiveEntry := range archiveEntries {
		// 顶层目录名已经由 archive entry 确定，无需为非递归列举下载和校验 zip。
		if request.Path == skillNamespacePath && !request.Recursive {
			nodes = append(nodes, skillArchiveNode{
				path:      archiveEntry.Path,
				directory: true,
			})
			continue
		}
		if request.Path != skillNamespacePath &&
			request.Path != archiveEntry.Path &&
			!strings.HasPrefix(request.Path, archiveEntry.Path+"/") {
			continue
		}
		archive, apiErr := b.loadSkillArchive(ctx, archiveEntry)
		if apiErr != nil {
			return listDirectoryResponse{}, apiErr
		}
		if node, ok := archive.nodes[request.Path]; ok && node.directory {
			directoryExists = true
		}
		for _, node := range archive.nodes {
			if node.path == request.Path {
				continue
			}
			if request.Recursive {
				if strings.HasPrefix(node.path, request.Path+"/") {
					nodes = append(nodes, node)
				}
				continue
			}
			if path.Dir(node.path) == request.Path {
				nodes = append(nodes, node)
			}
		}
	}
	if !directoryExists {
		return listDirectoryResponse{}, notFound("resource does not exist")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].path < nodes[j].path })
	if cursor.LastPath != "" {
		first := sort.Search(len(nodes), func(index int) bool {
			return nodes[index].path > cursor.LastPath
		})
		nodes = nodes[first:]
	}
	hasMore := len(nodes) > limit
	if hasMore {
		nodes = nodes[:limit]
	}
	response := listDirectoryResponse{Entries: make([]entryPayload, 0, len(nodes))}
	for _, node := range nodes {
		response.Entries = append(
			response.Entries,
			skillNodePayload(node, filesystem.ExternalID, archiveEntries),
		)
	}
	if hasMore {
		response.Cursor, err = encodeDirectoryCursor(directoryCursor{
			FilesystemID: request.FilesystemID,
			Path:         request.Path,
			Recursive:    request.Recursive,
			LastPath:     nodes[len(nodes)-1].path,
		})
		if err != nil {
			return listDirectoryResponse{}, internalError("encode directory cursor", err)
		}
	}
	return response, nil
}

func (b *skillArchivePathBackend) readMetadata(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (entryPayload, *apiError) {
	archiveEntry, _, node, apiErr := b.resolveSkillNode(ctx, principal, filesystem, entryPath)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	return skillNodePayload(node, filesystem.ExternalID, []db.FilestoreEntry{archiveEntry}), nil
}

func (b *skillArchivePathBackend) readFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request readFileRequest,
) (readFileResult, *apiError) {
	_, _, node, apiErr := b.resolveSkillNode(ctx, principal, filesystem, request.Path)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	if node.directory || node.file == nil {
		return readFileResult{}, failedPrecondition("path is not a file")
	}
	objectRange, responseSize, apiErr := resolveReadRange(request.Range, node.size)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	if responseSize == 0 {
		return readFileResult{
			Body:      io.NopCloser(bytes.NewReader(nil)),
			MediaType: node.mediaType,
		}, nil
	}
	reader, err := node.file.Open()
	if err != nil {
		return readFileResult{}, internalError("open skill archive member", err)
	}
	offset := int64(0)
	if objectRange != nil {
		offset = objectRange.Offset
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, reader, offset); err != nil {
			reader.Close()
			return readFileResult{}, internalError("seek skill archive member", err)
		}
	}
	return readFileResult{
		Body:      &limitedReadCloser{Reader: io.LimitReader(reader, responseSize), Closer: reader},
		Size:      responseSize,
		MediaType: node.mediaType,
	}, nil
}

func (b *skillArchivePathBackend) resolveSkillNode(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (db.FilestoreEntry, *loadedSkillArchive, skillArchiveNode, *apiError) {
	archiveEntries, err := b.db.ListFilestoreSkillArchiveEntries(ctx, principal.WorkspaceUUID, filesystem.UUID)
	if err != nil {
		return db.FilestoreEntry{}, nil, skillArchiveNode{}, mapDatabaseError("list skill archive entries", err)
	}
	for _, archiveEntry := range archiveEntries {
		if entryPath != archiveEntry.Path && !strings.HasPrefix(entryPath, archiveEntry.Path+"/") {
			continue
		}
		archive, apiErr := b.loadSkillArchive(ctx, archiveEntry)
		if apiErr != nil {
			return db.FilestoreEntry{}, nil, skillArchiveNode{}, apiErr
		}
		node, ok := archive.nodes[entryPath]
		if !ok {
			return db.FilestoreEntry{}, nil, skillArchiveNode{}, notFound("resource does not exist")
		}
		return archiveEntry, archive, node, nil
	}
	return db.FilestoreEntry{}, nil, skillArchiveNode{}, notFound("resource does not exist")
}

func (b *skillArchivePathBackend) loadSkillArchive(
	ctx context.Context,
	archiveEntry db.FilestoreEntry,
) (*loadedSkillArchive, *apiError) {
	if b.cache == nil {
		return nil, internalError("load skill archive", errors.New("skill archive cache is unavailable"))
	}
	bucket, objectKey, checksum, sizeBytes, err := skillArchiveObject(archiveEntry)
	if err != nil {
		return nil, internalError("load skill archive", err)
	}
	cacheKey := strings.Join([]string{bucket, objectKey, checksum, archiveEntry.Path}, "\x00")
	if archive, ok := b.cache.Get(cacheKey); ok {
		return archive, nil
	}
	// 已经取消的请求不应凭空创建一个没有等待者的后台加载；只有在注册 singleflight
	// 之后发生的取消，才允许共享任务继续服务其他调用者或预热缓存。
	if err := ctx.Err(); err != nil {
		return nil, skillArchiveRequestCanceled(err)
	}
	resultChannel := b.archiveLoads.DoChan(cacheKey, func() (any, error) {
		// 快路径 miss 后，可能已有同 key 的加载刚完成；进入 singleflight 后必须再次检查缓存。
		if archive, ok := b.cache.Get(cacheKey); ok {
			return archive, nil
		}

		// singleflight 的共享加载不能绑定首个调用者的取消信号。否则 leader 的客户端断开时，
		// S3 下载会被取消，仍在等待的其他请求也会收到同一个失败结果。WithoutCancel 保留
		// tracing 等 context values，但切断首个请求的取消和 deadline；随后再施加服务端自己的
		// 有界超时，避免所有调用者都离开后共享加载无限占用连接或 goroutine。
		loadCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			b.effectiveLoadTimeout(),
		)
		defer cancel()

		archive, apiErr := b.fetchSkillArchive(loadCtx, archiveEntry, bucket, objectKey, checksum, sizeBytes)
		if apiErr != nil {
			return nil, apiErr
		}
		b.cache.Add(cacheKey, archive)
		return archive, nil
	})

	// DoChan 让当前调用者可以独立响应自己的取消，而不终止共享加载。这里不能在取消时调用
	// Forget：共享任务仍可能服务其他 waiter 或填充缓存；Forget 会允许后续请求启动第二次
	// 同 key 下载，重新引入冷启动并发拉取。即使所有 waiter 都取消，共享任务也只会继续到
	// 下载完成或上面的服务端超时。
	select {
	case <-ctx.Done():
		return nil, skillArchiveRequestCanceled(ctx.Err())
	case result := <-resultChannel:
		return decodeSkillArchiveLoadResult(result.Val, result.Err)
	}
}

func (b *skillArchivePathBackend) effectiveLoadTimeout() time.Duration {
	if b.loadTimeout > 0 {
		return b.loadTimeout
	}
	return defaultSkillArchiveLoadTimeout
}

func decodeSkillArchiveLoadResult(value any, loadErr error) (*loadedSkillArchive, *apiError) {
	if loadErr != nil {
		apiErr, ok := loadErr.(*apiError)
		if !ok {
			return nil, internalError("load skill archive", loadErr)
		}
		return nil, apiErr
	}
	archive, ok := value.(*loadedSkillArchive)
	if !ok {
		return nil, internalError(
			"load skill archive",
			fmt.Errorf("unexpected archive load result type %T", value),
		)
	}
	return archive, nil
}

func skillArchiveRequestCanceled(cause error) *apiError {
	return &apiError{
		Status:  http.StatusRequestTimeout,
		Code:    "request_canceled",
		Message: "Skill archive request was canceled",
		Cause:   cause,
	}
}

func (b *skillArchivePathBackend) fetchSkillArchive(
	ctx context.Context,
	archiveEntry db.FilestoreEntry,
	bucket string,
	objectKey string,
	checksum string,
	sizeBytes int64,
) (*loadedSkillArchive, *apiError) {
	if b.store == nil {
		return nil, internalError("load skill archive", errors.New("object store is unavailable"))
	}
	if bucket != b.store.Name() {
		return nil, internalError("load skill archive", errors.New("skill archive bucket is unavailable"))
	}
	if sizeBytes > maxSkillArchiveBytes {
		return nil, internalError("load skill archive", errors.New("skill archive size is invalid"))
	}
	object, err := b.store.Open(ctx, objectKey, nil)
	if err != nil {
		return nil, mapBlobstoreError("load skill archive", err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, maxSkillArchiveBytes+1))
	if err != nil {
		return nil, mapBlobstoreError("read skill archive", err)
	}
	if int64(len(data)) != sizeBytes {
		return nil, internalError("validate skill archive", errors.New("skill archive size mismatch"))
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), checksum) {
		return nil, internalError("validate skill archive", errors.New("skill archive checksum mismatch"))
	}
	archive, err := indexSkillArchive(archiveEntry, data)
	if err != nil {
		return nil, internalError("validate skill archive", err)
	}
	return archive, nil
}

func skillArchiveObject(entry db.FilestoreEntry) (string, string, string, int64, error) {
	if entry.Kind != db.FilestoreEntryKindArchive ||
		entry.ManagedBy == nil ||
		*entry.ManagedBy != "skill_archive" ||
		entry.ManagedResourceUUID == nil ||
		entry.S3Bucket == nil ||
		entry.S3Key == nil ||
		entry.SHA256 == nil ||
		entry.SizeBytes == nil ||
		*entry.SizeBytes <= 0 {
		return "", "", "", 0, errors.New("skill archive entry is invalid")
	}
	return *entry.S3Bucket, *entry.S3Key, *entry.SHA256, *entry.SizeBytes, nil
}

func indexSkillArchive(archiveEntry db.FilestoreEntry, data []byte) (*loadedSkillArchive, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("skill archive is not a valid zip")
	}
	directory := strings.TrimPrefix(archiveEntry.Path, skillNamespacePath+"/")
	if directory == "" || strings.Contains(directory, "/") {
		return nil, errors.New("skill archive virtual path is invalid")
	}
	nodes := map[string]skillArchiveNode{
		archiveEntry.Path: {path: archiveEntry.Path, directory: true},
	}
	var totalUncompressed uint64
	hasSkillMD := false
	for _, file := range reader.File {
		cleanName, parts, err := validateSkillZipPath(file.Name)
		if err != nil {
			return nil, err
		}
		if parts[0] != directory {
			return nil, fmt.Errorf("skill archive top-level directory %q does not match %q", parts[0], directory)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("skill archive contains symlink %q", file.Name)
		}
		virtualPath := skillNamespacePath + "/" + strings.TrimSuffix(cleanName, "/")
		if file.FileInfo().IsDir() {
			if err := addSkillDirectoryNode(nodes, virtualPath); err != nil {
				return nil, err
			}
			continue
		}
		if file.UncompressedSize64 > math.MaxInt64 {
			return nil, fmt.Errorf("skill archive member %q is too large", file.Name)
		}
		next := totalUncompressed + file.UncompressedSize64
		if next < totalUncompressed || next > maxSkillUncompressedBytes {
			return nil, errors.New("skill archive uncompressed size exceeds limit")
		}
		totalUncompressed = next
		if err := addSkillParentNodes(nodes, path.Dir(virtualPath), archiveEntry.Path); err != nil {
			return nil, err
		}
		if previous, exists := nodes[virtualPath]; exists {
			if previous.directory {
				return nil, fmt.Errorf("skill archive path %q is both a file and directory", file.Name)
			}
			return nil, fmt.Errorf("skill archive contains duplicate path %q", file.Name)
		}
		mediaType := mime.TypeByExtension(path.Ext(virtualPath))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		nodes[virtualPath] = skillArchiveNode{
			path:      virtualPath,
			size:      int64(file.UncompressedSize64),
			mediaType: mediaType,
			file:      file,
		}
		if virtualPath == archiveEntry.Path+"/SKILL.md" {
			hasSkillMD = true
		}
	}
	if !hasSkillMD {
		return nil, fmt.Errorf("%s/SKILL.md not found", directory)
	}
	return &loadedSkillArchive{data: data, nodes: nodes}, nil
}

func validateSkillZipPath(name string) (string, []string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", nil, fmt.Errorf("invalid skill archive path %q", name)
	}
	cleanName := strings.TrimSuffix(name, "/")
	if cleanName == "" {
		return "", nil, fmt.Errorf("invalid skill archive path %q", name)
	}
	parts := strings.Split(cleanName, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, fmt.Errorf("invalid skill archive path %q", name)
		}
	}
	return name, parts, nil
}

func addSkillParentNodes(nodes map[string]skillArchiveNode, directoryPath, rootPath string) error {
	for directoryPath != "." && directoryPath != skillNamespacePath {
		if err := addSkillDirectoryNode(nodes, directoryPath); err != nil {
			return err
		}
		if directoryPath == rootPath {
			return nil
		}
		directoryPath = path.Dir(directoryPath)
	}
	return nil
}

func addSkillDirectoryNode(nodes map[string]skillArchiveNode, directoryPath string) error {
	if previous, exists := nodes[directoryPath]; exists && !previous.directory {
		return fmt.Errorf("skill archive path %q is both a file and directory", directoryPath)
	}
	nodes[directoryPath] = skillArchiveNode{path: directoryPath, directory: true}
	return nil
}

func skillNodePayload(
	node skillArchiveNode,
	filesystemExternalID string,
	archiveEntries []db.FilestoreEntry,
) entryPayload {
	createdAt := time.Unix(0, 0).UTC()
	nodeIdentity := ""
	for _, archiveEntry := range archiveEntries {
		if node.path == archiveEntry.Path || strings.HasPrefix(node.path, archiveEntry.Path+"/") {
			createdAt = archiveEntry.CreatedAt
			versionUUID := ""
			if archiveEntry.ManagedResourceUUID != nil {
				versionUUID = *archiveEntry.ManagedResourceUUID
			}
			nodeIdentity = strings.Join([]string{
				archiveEntry.FilesystemUUID,
				versionUUID,
			}, "\x00")
			break
		}
	}
	nodeUUID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(nodeIdentity+"\x00"+node.path)).String()
	if node.directory {
		directory := directoryPayload{
			FilesystemID: filesystemExternalID,
			Path:         node.path,
			CreatedAt:    formatTimestamp(createdAt),
		}
		return entryPayload{Directory: &directory}
	}
	file := filesystemFilePayload{
		FilesystemID: filesystemExternalID,
		Path:         node.path,
		File: filePayload{
			UUID:             nodeUUID,
			CreatedAt:        formatTimestamp(createdAt),
			Size:             protoInt64(node.size),
			MediaType:        node.mediaType,
			DetectedMimeType: node.mediaType,
			EntryTaggedID:    "fse_" + strings.ReplaceAll(nodeUUID, "-", ""),
			FilesystemID:     filesystemExternalID,
		},
	}
	return entryPayload{File: &file}
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}
