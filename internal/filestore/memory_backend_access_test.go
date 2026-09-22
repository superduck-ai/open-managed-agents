package filestore

import (
	"context"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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
				mounts: []db.SessionMemoryMount{mount},
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
