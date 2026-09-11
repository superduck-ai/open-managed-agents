package filestore

import (
	"sort"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type memoryDirectoryEntry struct {
	path      string
	uuid      string
	directory bool
	memory    db.Memory
}

func memoryListPrefix(dirRel string) string {
	if dirRel == "" {
		return ""
	}
	return dirRel + "/"
}

func collectMemoryDirectoryEntries(parsed memoryFilestorePath, recursive bool, records []db.Memory) []memoryDirectoryEntry {
	seenDirs := make(map[string]struct{})
	entries := make([]memoryDirectoryEntry, 0)
	for _, record := range records {
		relative, ok := memoryPathRelativeToDir(parsed.Rel, record.Path)
		if !ok {
			continue
		}
		filestoreBase := parsed.filestorePath()
		if recursive {
			entries = append(entries, memoryDirectoryEntry{
				path:   filestoreBase + relative,
				uuid:   record.UUID,
				memory: record,
			})
			continue
		}
		name, _, hasChildren := strings.Cut(strings.TrimPrefix(relative, "/"), "/")
		childPath := filestoreBase + "/" + name
		if hasChildren {
			if _, seen := seenDirs[childPath]; seen {
				continue
			}
			seenDirs[childPath] = struct{}{}
			entries = append(entries, memoryDirectoryEntry{path: childPath, uuid: name, directory: true})
			continue
		}
		entries = append(entries, memoryDirectoryEntry{
			path:   childPath,
			uuid:   record.UUID,
			memory: record,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path == entries[j].path {
			return entries[i].uuid < entries[j].uuid
		}
		return entries[i].path < entries[j].path
	})
	return entries
}

func memoryPathRelativeToDir(dirRel, memoryPath string) (string, bool) {
	if dirRel == "" {
		return memoryPath, strings.HasPrefix(memoryPath, "/")
	}
	if !strings.HasPrefix(memoryPath, dirRel+"/") {
		return "", false
	}
	return strings.TrimPrefix(memoryPath, dirRel), true
}

func paginateMemoryDirectory(
	request listDirectoryRequest,
	cursor directoryCursor,
	limit int,
	entries []memoryDirectoryEntry,
	filesystemID string,
) (listDirectoryResponse, *apiError) {
	start := 0
	if cursor.LastPath != "" {
		for index, entry := range entries {
			if entry.path > cursor.LastPath || (entry.path == cursor.LastPath && entry.uuid > cursor.LastUUID) {
				start = index
				break
			}
			start = index + 1
		}
	}
	if start > len(entries) {
		start = len(entries)
	}
	end := start + limit
	hasMore := end < len(entries)
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[start:end]
	response := listDirectoryResponse{Entries: make([]entryPayload, 0, len(page))}
	for _, entry := range page {
		if entry.directory {
			directory := virtualMemoryDirectory(filesystemID, entry.path, time.Unix(0, 0).UTC())
			response.Entries = append(response.Entries, entryPayload{Directory: &directory})
			continue
		}
		file := memoryFilePayload(entry.memory, filesystemID, entry.path, "text/plain")
		response.Entries = append(response.Entries, entryPayload{File: &file})
	}
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		encoded, err := encodeDirectoryCursor(directoryCursor{
			FilesystemID: request.FilesystemID,
			Path:         request.Path,
			Recursive:    request.Recursive,
			LastPath:     last.path,
			LastUUID:     last.uuid,
		})
		if err != nil {
			return listDirectoryResponse{}, internalError("encode directory cursor", err)
		}
		response.Cursor = encoded
	}
	return response, nil
}
