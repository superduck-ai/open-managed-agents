package devinit

import (
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T, repo string) Registry {
	t.Helper()
	initGitRepo(t, repo)
	registry, err := OpenRegistry(repo)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestRegistryCreateAssignsUniqueName(t *testing.T) {
	repo := t.TempDir()
	registry := newTestRegistry(t, repo)
	binding, entry, err := registry.Create(repo, "/repo/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Name != "feature-x" {
		t.Fatalf("binding = %+v", binding)
	}
	if entry.EnvID == "" || entry.Worktree != repo {
		t.Fatalf("entry = %+v", entry)
	}
	second, _, err := registry.Create(filepath.Join(repo, "second"), "/second/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if second.Name == binding.Name {
		t.Fatalf("second binding = %+v, want unique name", second)
	}
}

func TestRegistryDoesNotReuseOrphansBeforeTeardown(t *testing.T) {
	repo := t.TempDir()
	registry := newTestRegistry(t, repo)
	gone := filepath.Join(t.TempDir(), "deleted-worktree")
	first, _, err := registry.Create(gone, "/gone/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := registry.Create(repo, "/repo/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if second.Name == first.Name {
		t.Fatalf("orphan name %q must stay reserved until teardown", first.Name)
	}
	if err := registry.Remove(first.Name); err != nil {
		t.Fatal(err)
	}
	third, _, err := registry.Create(filepath.Join(repo, "third"), "/repo/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if third.Name != first.Name {
		t.Fatalf("after teardown want name %q, got %q", first.Name, third.Name)
	}
}

func TestRegistryOrphansAndRemove(t *testing.T) {
	repo := t.TempDir()
	registry := newTestRegistry(t, repo)
	gone := filepath.Join(t.TempDir(), "deleted-worktree")
	binding, _, err := registry.Create(gone, "/gone/dev-init.yaml", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Create(repo, "/repo/dev-init.yaml", "other"); err != nil {
		t.Fatal(err)
	}
	orphans, err := registry.Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[binding.Name].EnvID == "" {
		t.Fatalf("Orphans() = %+v, want only %s", orphans, binding.Name)
	}
	if err := registry.Remove(binding.Name); err != nil {
		t.Fatal(err)
	}
	orphans, err = registry.Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("Orphans() after Remove = %+v, want empty", orphans)
	}
}
