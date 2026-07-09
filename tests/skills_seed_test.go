package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestSeedBuiltinSkillsRejectsInvalidArchives(t *testing.T) {
	store := newFakeStore("seed-invalid-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()
	cleanupBuiltinSkillRows(t, app.db)
	defer cleanupBuiltinSkillRows(t, app.db)

	tests := []struct {
		name    string
		entries map[string]string
		want    string
	}{
		{
			name: "missing skill md",
			entries: map[string]string{
				"bad/README.md": "# Bad",
			},
			want: "SKILL.md",
		},
		{
			name: "multiple top-level directories",
			entries: map[string]string{
				"one/SKILL.md": "# One",
				"two/SKILL.md": "# Two",
			},
			want: "single top-level directory",
		},
		{
			name: "path traversal",
			entries: map[string]string{
				"../bad/SKILL.md": "# Bad",
			},
			want: "Invalid skill file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSkillArchive(t, dir, uniqueBuiltinSeedID("bad"), zipEntriesBytes(t, tt.entries))
			_, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{Dir: dir})
			if err == nil || !stringsContains(err.Error(), tt.want) {
				t.Fatalf("seed invalid archive err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSeedBuiltinSkills(t *testing.T) {
	store := newFakeStore("seed-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()
	cleanupBuiltinSkillRows(t, app.db)
	defer cleanupBuiltinSkillRows(t, app.db)

	dir := t.TempDir()
	skillID := uniqueBuiltinSeedID("xlsx")
	writeSkillArchive(t, dir, skillID, skillArchiveBytes(t, skillID, "---\nname: xlsx\ndescription: spreadsheets\n---\n\n# xlsx\n"))
	versionsPath := filepath.Join(dir, "versions.json")
	if err := os.WriteFile(versionsPath, []byte(fmt.Sprintf("%s=20260203\n", skillID)), 0o600); err != nil {
		t.Fatalf("write versions: %v", err)
	}

	result, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{
		Dir:          dir,
		VersionsPath: versionsPath,
	})
	if err != nil {
		t.Fatalf("seed builtin skills: %v", err)
	}
	if result.Imported != 1 || len(result.Skills) != 1 || result.Skills[0] != skillID {
		t.Fatalf("seed result = %+v", result)
	}
	skill, err := app.db.GetBuiltinSkill(context.Background(), skillID)
	if err != nil {
		t.Fatalf("get seeded builtin skill: %v", err)
	}
	if skill.LatestVersion == nil || *skill.LatestVersion != "20260203" {
		t.Fatalf("latest version = %v, want 20260203", skill.LatestVersion)
	}
	version, err := app.db.GetBuiltinSkillVersion(context.Background(), skillID, "latest")
	if err != nil {
		t.Fatalf("get seeded builtin version: %v", err)
	}
	if _, ok := store.objects[version.S3Key]; !ok {
		t.Fatalf("seeded object %s missing from store", version.S3Key)
	}

	if _, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{
		Dir:          dir,
		VersionsPath: versionsPath,
	}); err != nil {
		t.Fatalf("seed builtin skills idempotent rerun: %v", err)
	}
	rerunVersion, err := app.db.GetBuiltinSkillVersion(context.Background(), skillID, "latest")
	if err != nil {
		t.Fatalf("get seeded builtin version after rerun: %v", err)
	}
	if !rerunVersion.CreatedAt.Equal(version.CreatedAt) {
		t.Fatalf("version created_at changed on idempotent rerun: got %s, want %s", rerunVersion.CreatedAt, version.CreatedAt)
	}

	writeSkillArchive(t, dir, skillID, skillArchiveBytes(t, skillID, "---\nname: xlsx\ndescription: changed\n---\n\n# xlsx\n"))
	if _, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{
		Dir:          dir,
		VersionsPath: versionsPath,
	}); err == nil || !stringsContains(err.Error(), "already exists with different content") {
		t.Fatalf("conflicting seed error = %v, want version conflict", err)
	}
}

func TestSeedBuiltinSkillsPrune(t *testing.T) {
	store := newFakeStore("seed-prune-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()
	cleanupBuiltinSkillRows(t, app.db)
	defer cleanupBuiltinSkillRows(t, app.db)

	dir := t.TempDir()
	xlsxID := uniqueBuiltinSeedID("xlsx")
	pdfID := uniqueBuiltinSeedID("pdf")
	writeSkillArchive(t, dir, xlsxID, skillArchiveBytes(t, xlsxID, "# xlsx\n"))
	writeSkillArchive(t, dir, pdfID, skillArchiveBytes(t, pdfID, "# pdf\n"))
	if _, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{Dir: dir}); err != nil {
		t.Fatalf("seed initial: %v", err)
	}
	pdfVersion, err := app.db.GetBuiltinSkillVersion(context.Background(), pdfID, "latest")
	if err != nil {
		t.Fatalf("get seeded pdf version: %v", err)
	}
	if _, ok := store.objects[pdfVersion.S3Key]; !ok {
		t.Fatalf("seeded pdf object %s missing from store", pdfVersion.S3Key)
	}
	if err := os.Remove(filepath.Join(dir, pdfID+".skill")); err != nil {
		t.Fatalf("remove pdf archive: %v", err)
	}
	result, err := skills.SeedBuiltinSkills(context.Background(), app.db, store, skills.BuiltinSeedOptions{Dir: dir, Prune: true})
	if err != nil {
		t.Fatalf("seed prune: %v", err)
	}
	if result.Pruned != 1 {
		t.Fatalf("pruned = %d, want 1", result.Pruned)
	}
	if _, err := app.db.GetBuiltinSkill(context.Background(), pdfID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("get pruned pdf err = %v, want not found", err)
	}
	if _, ok := store.objects[pdfVersion.S3Key]; ok {
		t.Fatalf("pruned pdf object %s still present in store", pdfVersion.S3Key)
	}
}

func cleanupBuiltinSkillRows(t *testing.T, database *db.DB) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `delete from builtin_skill_versions`); err != nil {
		t.Fatalf("cleanup builtin skill versions: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from builtin_skills`); err != nil {
		t.Fatalf("cleanup builtin skills: %v", err)
	}
}

func uniqueBuiltinSeedID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func writeSkillArchive(t *testing.T, dir, id string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".skill"), data, 0o600); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}
}

func zipEntriesBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func stringsContains(value, needle string) bool {
	return strings.Contains(value, needle)
}
