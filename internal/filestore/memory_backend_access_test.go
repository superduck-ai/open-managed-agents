package filestore

import (
	"context"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestResolveMemoryMountRejectsDuplicateSlugs(t *testing.T) {
	t.Parallel()

	first := db.SessionMemoryMount{
		MemoryStoreUUID: "store-a", MountPath: "/mnt/memory/slug", Access: "read_write",
	}
	for _, scenario := range []struct {
		name   string
		second db.SessionMemoryMount
	}{
		{name: "different stores", second: db.SessionMemoryMount{MemoryStoreUUID: "store-b", Access: "read_write"}},
		{name: "same store", second: first},
		{name: "read only", second: db.SessionMemoryMount{MemoryStoreUUID: "store-b", Access: "read_only"}},
		{name: "archived", second: db.SessionMemoryMount{MemoryStoreUUID: "store-b", Archived: true}},
		{name: "missing store", second: db.SessionMemoryMount{StoreMissing: true}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			second := scenario.second
			second.MountPath = first.MountPath
			for _, mounts := range [][]db.SessionMemoryMount{{first, second}, {second, first}} {
				backend := &memoryPathBackend{memories: &recordingMemoryStore{mounts: mounts}}
				for _, mutate := range []bool{false, true} {
					got, apiErr := backend.resolveMount(context.Background(), Principal{WorkspaceUUID: "ws"},
						db.FilestoreFilesystem{UUID: "fs-uuid"}, memoryFilestorePath{Slug: "slug"}, mutate)
					if apiErr == nil || apiErr.Status != http.StatusConflict || apiErr.Code != "failed_precondition" || apiErr.Message != "multiple memory mounts match the requested slug" {
						t.Fatalf("mounts=%+v mutate=%v: want duplicate mount error, got mount %+v, error %v", mounts, mutate, got, apiErr)
					}
					if got.MemoryStoreUUID != "" {
						t.Fatalf("duplicate mount returned a store: %+v", got)
					}
				}
			}
		})
	}
}

func TestResolveMemoryMountAccess(t *testing.T) {
	t.Parallel()

	for _, access := range []string{"read_only", "readonly", "unknown", "", "READ_WRITE", " read_write ", "read_write"} {
		t.Run("access="+access, func(t *testing.T) {
			t.Parallel()
			mount := db.SessionMemoryMount{
				MemoryStoreUUID:       "store-uuid",
				MemoryStoreExternalID: "memstore_1",
				MountPath:             "/mnt/memory/slug",
				Access:                access,
			}
			backend := &memoryPathBackend{memories: &recordingMemoryStore{
				mounts: []db.SessionMemoryMount{
					{MemoryStoreUUID: "other-store", MountPath: "/mnt/memory/other"},
					mount,
				},
			}}
			for _, mutate := range []bool{true, false} {
				got, apiErr := backend.resolveMount(context.Background(), Principal{WorkspaceUUID: "ws"},
					db.FilestoreFilesystem{UUID: "fs-uuid"}, memoryFilestorePath{Slug: "slug"}, mutate)
				if mutate && access != "read_write" {
					if apiErr == nil || apiErr.Status != http.StatusForbidden || apiErr.Code != "permission_denied" {
						t.Fatalf("write with access %q: want permission_denied (403), got %v", access, apiErr)
					}
					continue
				}
				if apiErr != nil || got.MemoryStoreUUID != mount.MemoryStoreUUID || got.MemoryStoreExternalID != mount.MemoryStoreExternalID || got.MountPath != mount.MountPath || got.Access != mount.Access {
					t.Fatalf("mutate=%v: got mount %+v, error %v; want %+v", mutate, got, apiErr, mount)
				}
			}
		})
	}
}
