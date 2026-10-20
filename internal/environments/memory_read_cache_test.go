package environments

import (
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
	"testing"
)

func TestMemoryMountReadsBypassDiskCacheRetainingWriteBuffering(t *testing.T) {
	config := buildRcloneMultimountConfig("fs_owned", "http://owned.test", "rw", "ro", []memoryRuntimeMount{
		{Slug: "rw", MountPath: "/mnt/memory/rw", Access: sessionresource.MemoryAccessReadWrite},
		{Slug: "ro", MountPath: "/mnt/memory/ro", Access: sessionresource.MemoryAccessReadOnly},
	})
	if len(config.Mounts) != 7 {
		t.Fatalf("unexpected mount count%d", len(config.Mounts))
	}
	for i, mount := range config.Mounts {
		if i < 5 {
			if mount.VFSCacheMode != "full" {
				t.Fatalf("fixed mount changed cache policy: %+v", mount)
			}
			continue
		}
		if mount.VFSCacheMode != "writes" || mount.CacheDurationSeconds != 1 {
			t.Fatalf("Memory retains stale read cache or loses bounded metadata caching: %+v", mount)
		}
		if mount.Readonly != (i == 6) || (i == 6 && mount.AuthToken != "ro") || (i == 5 && mount.AuthToken != "rw") {
			t.Fatalf("Memory access authority changed: %+v", mount)
		}
	}
}
