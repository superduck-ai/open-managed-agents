package filestore

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

func TestMemoryFileMetadataRetainsLogicalCreationTime(t *testing.T) {
	created := time.Date(2026, 10, 19, 3, 0, 0, 100, time.UTC)
	record := db.Memory{UUID: "stable-memory-uuid", ExternalID: "mem_stable", CreatedAt: created, UpdatedAt: created, ContentSizeBytes: 40}
	before := memoryFilePayload(record, "fs_owned", "/memory/owned/note.txt", "text/plain")
	record.UpdatedAt = created.Add(100 * time.Nanosecond)
	after := memoryFilePayload(record, "fs_owned", "/memory/owned/note.txt", "text/plain")
	if before.File.CreatedAt != after.File.CreatedAt || after.File.CreatedAt != created.Format(time.RFC3339Nano) {
		t.Fatalf("filesystem metadata changed logical creation time: before=%+v after=%+v", before, after)
	}
	if after.File.UUID != before.File.UUID || after.File.EntryTaggedID != before.File.EntryTaggedID || after.Path != before.Path || after.File.Size != before.File.Size || !record.CreatedAt.Equal(created) {
		t.Fatal("cache refresh changed logical identity, size, path, or Memory creation time")
	}
}
