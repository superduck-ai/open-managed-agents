package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"io"
	"testing"
)

func TestSkillArchiveCacheDoesNotBypassSourceDirectoryValidation(t *testing.T) {
	data := buildSkillArchiveTestZip(t, map[string]string{"demo/SKILL.md": "# Demo"})
	entry := skillArchiveTestEntry(data)
	backend := &skillArchivePathBackend{cache: newSkillArchiveCache(4), store: &fakeServiceBlobStore{openFn: func(context.Context, string, *storage.ByteRange) (storage.Object, error) {
		return storage.Object{Body: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data))}, nil
	}}}
	entry.Metadata = json.RawMessage(`{"skill_archive_directory":"demo"}`)
	if _, err := backend.loadSkillArchive(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	entry.Metadata = json.RawMessage(`{"skill_archive_directory":"foreign"}`)
	if _, err := backend.loadSkillArchive(context.Background(), entry); err == nil {
		t.Fatal("cached archive bypassed changed source-directory validation")
	}
}

func TestImmutableSkillArchivesProjectIntoSeparateVersionMounts(t *testing.T) {
	for _, version := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		mount := "/skills/shared--" + version
		data := buildSkillArchiveTestZip(t, map[string]string{"shared/SKILL.md": "---\nname: shared\n---\n", "shared/references/value.txt": version})
		archive, err := indexSkillArchive(db.SessionResourceFile{Path: mount, Metadata: json.RawMessage(`{"skill_archive_directory":"shared"}`)}, data)
		if err != nil {
			t.Fatal(err)
		}
		node, ok := archive.nodes[mount+"/references/value.txt"]
		if !ok {
			t.Fatal(archive.nodes)
		}
		reader, err := node.file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(body) != version {
			t.Fatal(string(body), err)
		}
		if _, ok := archive.nodes["/skills/shared/references/value.txt"]; ok {
			t.Fatal("original directory leaked into version mount")
		}
		if _, err := indexSkillArchive(db.SessionResourceFile{Path: mount, Metadata: json.RawMessage(`{"skill_archive_directory":"foreign"}`)}, data); err == nil {
			t.Fatal("foreign archive source accepted")
		}
	}
}
