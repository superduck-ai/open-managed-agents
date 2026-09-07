package filestore

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCollectMemoryDirectoryEntries(t *testing.T) {
	t.Parallel()

	parsed := memoryFilestorePath{Slug: "test"}
	records := []db.Memory{
		{UUID: "file-a", Path: "/notes/a.txt"},
		{UUID: "file-b", Path: "/notes/sub/b.txt"},
		{UUID: "root", Path: "/root.txt"},
	}

	entries := collectMemoryDirectoryEntries(parsed, false, records)
	if len(entries) != 2 {
		t.Fatalf("root listing = %#v", entries)
	}
	if !entries[0].directory || entries[0].path != "/memory/test/notes" {
		t.Fatalf("want notes directory first, got %#v", entries[0])
	}
	if entries[1].directory || entries[1].path != "/memory/test/root.txt" {
		t.Fatalf("want root.txt file, got %#v", entries[1])
	}

	nested := collectMemoryDirectoryEntries(memoryFilestorePath{Slug: "test", Rel: "/notes"}, false, records)
	if len(nested) != 2 {
		t.Fatalf("notes listing = %#v", nested)
	}
}
