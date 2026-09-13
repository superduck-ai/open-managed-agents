package filestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMoveFileLeavesDestinationWhenRenameFails(t *testing.T) {
	t.Parallel()

	store := &recordingMemoryStore{
		byPath: map[string]db.Memory{
			"/notes/a.txt": {UUID: "src-uuid", ExternalID: "mem_src", Path: "/notes/a.txt"},
			"/notes/b.txt": {UUID: "dst-uuid", ExternalID: "mem_dst", Path: "/notes/b.txt"},
		},
		mounts: []db.SessionMemoryMount{{
			SessionExternalID:     "sess_1",
			MemoryStoreExternalID: "memstore_1",
			MemoryStoreUUID:       "store-uuid",
			MountPath:             "/mnt/memory/slug",
			Access:                "read_write",
		}},
		updateErr: errors.New("rename failed"),
	}
	backend := &memoryPathBackend{
		memories: store,
		now:      func() time.Time { return time.Unix(1, 0).UTC() },
	}

	_, apiErr := backend.moveFile(
		context.Background(),
		Principal{WorkspaceUUID: "ws"},
		db.FilestoreFilesystem{UUID: "fs-uuid", ExternalID: "fse_1"},
		memoryFilestorePath{Slug: "slug", Rel: "/notes/a.txt"},
		memoryFilestorePath{Slug: "slug", Rel: "/notes/b.txt"},
	)
	if apiErr == nil {
		t.Fatal("want rename failure")
	}
	if _, found, err := store.GetMemoryByPath(context.Background(), "ws", "memstore_1", "/notes/b.txt"); err != nil || !found {
		t.Fatal("overwrite move deleted the destination before rename committed")
	}
	if _, found, err := store.GetMemoryByPath(context.Background(), "ws", "memstore_1", "/notes/a.txt"); err != nil || !found {
		t.Fatal("source should remain when rename fails")
	}
}

type recordingMemoryStore struct {
	mounts    []db.SessionMemoryMount
	byPath    map[string]db.Memory
	updateErr error
}

func (s *recordingMemoryStore) ListSessionMemoryMounts(context.Context, string, string) ([]db.SessionMemoryMount, error) {
	return s.mounts, nil
}

func (s *recordingMemoryStore) GetMemoryByPath(_ context.Context, _, _, path string) (db.Memory, bool, error) {
	record, found := s.byPath[path]
	return record, found, nil
}

func (s *recordingMemoryStore) ListMemoriesPage(context.Context, db.ListMemoriesPageParams) ([]db.Memory, bool, error) {
	panic("unexpected ListMemoriesPage")
}

func (s *recordingMemoryStore) ListMemoriesForDepth(context.Context, db.ListMemoriesPageParams) ([]db.Memory, error) {
	panic("unexpected ListMemoriesForDepth")
}

func (s *recordingMemoryStore) CreateMemory(context.Context, db.Memory, db.MemoryVersion) (db.Memory, error) {
	panic("unexpected CreateMemory")
}

func (s *recordingMemoryStore) UpdateMemory(context.Context, db.UpdateMemoryInput) (db.MemoryMutationResult, error) {
	if s.updateErr != nil {
		return db.MemoryMutationResult{}, s.updateErr
	}
	return db.MemoryMutationResult{}, nil
}

func (s *recordingMemoryStore) MoveMemory(_ context.Context, input db.MoveMemoryInput) (db.MemoryMutationResult, error) {
	if s.updateErr != nil {
		return db.MemoryMutationResult{}, s.updateErr
	}
	var current db.Memory
	var sourcePath string
	for path, record := range s.byPath {
		if record.ExternalID == input.SourceMemoryExternalID {
			current = record
			sourcePath = path
			break
		}
	}
	if sourcePath == "" {
		return db.MemoryMutationResult{}, db.ErrNotFound
	}
	delete(s.byPath, sourcePath)
	delete(s.byPath, input.DestinationPath)
	current.Path = input.DestinationPath
	s.byPath[input.DestinationPath] = current
	return db.MemoryMutationResult{Memory: current, VersionCreated: true}, nil
}

func (s *recordingMemoryStore) DeleteMemory(_ context.Context, input db.DeleteMemoryInput) error {
	for path, record := range s.byPath {
		if record.ExternalID == input.MemoryExternalID {
			delete(s.byPath, path)
			return nil
		}
	}
	return db.ErrNotFound
}
