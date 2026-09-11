package filestore

import (
	"testing"
)

func TestParseMemoryFilestorePath(t *testing.T) {
	t.Run("does not claim the parent directory or files without a slug", func(t *testing.T) {
		for _, path := range []string{
			"/memory",
			"/memory/MEMORY.md",
			"/memory/foo.md",
			"/outputs/notes.txt",
			"/mnt/memory/test",
		} {
			if _, claimed := parseMemoryFilestorePath(path); claimed {
				t.Fatalf("path %q should not be claimed by /memory/{slug}", path)
			}
		}
	})

	t.Run("claims attached-style slugs and nested files", func(t *testing.T) {
		root, claimed := parseMemoryFilestorePath("/memory/test")
		if !claimed || root.Slug != "test" || root.Rel != "" {
			t.Fatalf("parse /memory/test = %+v claimed=%t", root, claimed)
		}
		file, claimed := parseMemoryFilestorePath("/memory/test/notes/a.txt")
		if !claimed || file.Slug != "test" || file.Rel != "/notes/a.txt" {
			t.Fatalf("parse nested file = %+v claimed=%t", file, claimed)
		}
		unmounted, claimed := parseMemoryFilestorePath("/memory/not-attached/secret.md")
		if !claimed || unmounted.Slug != "not-attached" || unmounted.Rel != "/secret.md" {
			t.Fatalf("parse unmounted slug = %+v claimed=%t", unmounted, claimed)
		}
	})

	t.Run("slug is taken from the sandbox mount_path snapshot", func(t *testing.T) {
		if got := memorySlugFromMountPath("/mnt/memory/product-docs-draft"); got != "product-docs-draft" {
			t.Fatalf("slug = %q", got)
		}
		if got := memorySlugFromMountPath("/memory/test"); got != "" {
			t.Fatalf("filestore path is not a sandbox mount: %q", got)
		}
	})
}
