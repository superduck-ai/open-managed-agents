package filestore

import (
	"archive/zip"
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	skillNamespacePath                   = "/skills"
	maxSkillArchiveBytes          int64  = 8 * 1024 * 1024
	maxSkillUncompressedBytes     uint64 = 500 * 1024 * 1024
	defaultSkillArchiveCacheBytes        = 64 * 1024 * 1024
)

type skillArchiveNode struct {
	path      string
	directory bool
	size      int64
	mediaType string
	file      *zip.File
}

type loadedSkillArchive struct {
	projection db.FilestoreSkillArchive
	data       []byte
	nodes      map[string]skillArchiveNode
}

type skillArchiveCacheEntry struct {
	key     string
	archive *loadedSkillArchive
}

type skillArchiveCache struct {
	mu       sync.Mutex
	maxBytes int
	bytes    int
	entries  map[string]*list.Element
	order    *list.List
}

func newSkillArchiveCache(maxBytes int) *skillArchiveCache {
	return &skillArchiveCache{
		maxBytes: maxBytes,
		entries:  make(map[string]*list.Element),
		order:    list.New(),
	}
}

func (c *skillArchiveCache) get(key string) (*loadedSkillArchive, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(element)
	return element.Value.(skillArchiveCacheEntry).archive, true
}

func (c *skillArchiveCache) put(key string, archive *loadedSkillArchive) {
	if c == nil || archive == nil || len(archive.data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.bytes -= len(element.Value.(skillArchiveCacheEntry).archive.data)
		element.Value = skillArchiveCacheEntry{key: key, archive: archive}
		c.bytes += len(archive.data)
		c.order.MoveToFront(element)
	} else {
		element := c.order.PushFront(skillArchiveCacheEntry{key: key, archive: archive})
		c.entries[key] = element
		c.bytes += len(archive.data)
	}
	for c.bytes > c.maxBytes {
		element := c.order.Back()
		if element == nil {
			break
		}
		entry := element.Value.(skillArchiveCacheEntry)
		delete(c.entries, entry.key)
		c.bytes -= len(entry.archive.data)
		c.order.Remove(element)
	}
}

func isSkillNamespacePath(value string) bool {
	return value == skillNamespacePath || strings.HasPrefix(value, skillNamespacePath+"/")
}

func (s *Service) listSkillDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request listDirectoryRequest,
	cursor directoryCursor,
	limit int,
) (listDirectoryResponse, *apiError) {
	archives, err := s.db.ListFilestoreSkillArchives(ctx, principal.WorkspaceID, filesystem.ID)
	if err != nil {
		return listDirectoryResponse{}, mapDatabaseError("list skill archives", err)
	}
	nodes := make([]skillArchiveNode, 0)
	directoryExists := request.Path == skillNamespacePath
	for _, projection := range archives {
		if request.Path != skillNamespacePath &&
			request.Path != projection.VirtualPath &&
			!strings.HasPrefix(request.Path, projection.VirtualPath+"/") {
			continue
		}
		archive, apiErr := s.loadSkillArchive(ctx, projection)
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
		response.Entries = append(response.Entries, skillNodePayload(node, filesystem.ExternalID, archives))
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

func (s *Service) readSkillMetadata(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (entryPayload, *apiError) {
	archive, node, apiErr := s.resolveSkillNode(ctx, principal, filesystem, entryPath)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	return skillNodePayload(node, filesystem.ExternalID, []db.FilestoreSkillArchive{archive.projection}), nil
}

func (s *Service) readSkillFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request readFileRequest,
) (readFileResult, *apiError) {
	_, node, apiErr := s.resolveSkillNode(ctx, principal, filesystem, request.Path)
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

func (s *Service) resolveSkillNode(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (*loadedSkillArchive, skillArchiveNode, *apiError) {
	archives, err := s.db.ListFilestoreSkillArchives(ctx, principal.WorkspaceID, filesystem.ID)
	if err != nil {
		return nil, skillArchiveNode{}, mapDatabaseError("list skill archives", err)
	}
	for _, projection := range archives {
		if entryPath != projection.VirtualPath && !strings.HasPrefix(entryPath, projection.VirtualPath+"/") {
			continue
		}
		archive, apiErr := s.loadSkillArchive(ctx, projection)
		if apiErr != nil {
			return nil, skillArchiveNode{}, apiErr
		}
		node, ok := archive.nodes[entryPath]
		if !ok {
			return nil, skillArchiveNode{}, notFound("resource does not exist")
		}
		return archive, node, nil
	}
	return nil, skillArchiveNode{}, notFound("resource does not exist")
}

func (s *Service) loadSkillArchive(
	ctx context.Context,
	projection db.FilestoreSkillArchive,
) (*loadedSkillArchive, *apiError) {
	if s.skillArchives == nil {
		return nil, internalError("load skill archive", errors.New("skill archive cache is unavailable"))
	}
	cacheKey := strings.Join([]string{projection.S3Bucket, projection.S3Key, projection.SHA256}, "\x00")
	if archive, ok := s.skillArchives.get(cacheKey); ok {
		return archive, nil
	}
	if s.store == nil {
		return nil, internalError("load skill archive", errors.New("object store is unavailable"))
	}
	if projection.S3Bucket != s.store.Name() {
		return nil, internalError("load skill archive", errors.New("skill archive bucket is unavailable"))
	}
	if projection.SizeBytes <= 0 || projection.SizeBytes > maxSkillArchiveBytes {
		return nil, internalError("load skill archive", errors.New("skill archive size is invalid"))
	}
	object, err := s.store.Open(ctx, projection.S3Key, nil)
	if err != nil {
		return nil, mapBlobstoreError("load skill archive", err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, maxSkillArchiveBytes+1))
	if err != nil {
		return nil, mapBlobstoreError("read skill archive", err)
	}
	if int64(len(data)) != projection.SizeBytes {
		return nil, internalError("validate skill archive", errors.New("skill archive size mismatch"))
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), projection.SHA256) {
		return nil, internalError("validate skill archive", errors.New("skill archive checksum mismatch"))
	}
	archive, err := indexSkillArchive(projection, data)
	if err != nil {
		return nil, internalError("validate skill archive", err)
	}
	s.skillArchives.put(cacheKey, archive)
	return archive, nil
}

func indexSkillArchive(projection db.FilestoreSkillArchive, data []byte) (*loadedSkillArchive, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("skill archive is not a valid zip")
	}
	directory := strings.TrimPrefix(projection.VirtualPath, skillNamespacePath+"/")
	if directory == "" || strings.Contains(directory, "/") {
		return nil, errors.New("skill archive virtual path is invalid")
	}
	nodes := map[string]skillArchiveNode{
		projection.VirtualPath: {path: projection.VirtualPath, directory: true},
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
		if err := addSkillParentNodes(nodes, path.Dir(virtualPath), projection.VirtualPath); err != nil {
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
		if virtualPath == projection.VirtualPath+"/SKILL.md" {
			hasSkillMD = true
		}
	}
	if !hasSkillMD {
		return nil, fmt.Errorf("%s/SKILL.md not found", directory)
	}
	return &loadedSkillArchive{projection: projection, data: data, nodes: nodes}, nil
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
	archives []db.FilestoreSkillArchive,
) entryPayload {
	createdAt := time.Unix(0, 0).UTC()
	nodeIdentity := ""
	for _, projection := range archives {
		if node.path == projection.VirtualPath || strings.HasPrefix(node.path, projection.VirtualPath+"/") {
			createdAt = projection.CreatedAt
			nodeIdentity = strings.Join([]string{
				projection.FilesystemUUID,
				projection.Source,
				projection.SkillVersionUUID,
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
