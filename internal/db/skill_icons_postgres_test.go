//go:build integration

package db

import (
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestSkillIconsMigrationAndCatalog(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}

	bundled := SystemSkill{UUID: uuid.New().String(), ExternalID: "xlsx", DisplayTitle: "Excel 表格", IconKey: "chart-no-axes-combined"}
	if err := store.SyncBundledSkill(ctx, bundled, systemSkillRegressionVersion("xlsx", "bundled-v1", time.Now())); err != nil {
		t.Fatal(err)
	}
	rows, counts, err := store.SkillCatalog(ctx, SkillCatalogQuery{WorkspaceUUID: "20000000-0000-4000-8000-000000000001", Query: "Regression", Limit: 12})
	if err != nil || len(rows) != 1 || counts.Builtin != 1 || rows[0].IconKey != "chart-no-axes-combined" {
		t.Fatalf("listed bundled catalog: %+v %+v %v", rows, counts, err)
	}

	workspace := "20000000-0000-4000-8000-000000000001"
	custom, _, err := store.CreateSkillWithVersion(ctx, Skill{UUID: uuid.New().String(), ExternalID: "skill_custom", WorkspaceUUID: workspace, DisplayTitle: new("Custom"), IconKey: "image", CreatedAt: time.Now()}, SkillVersion{UUID: uuid.New().String(), ExternalID: "skver_custom", WorkspaceUUID: workspace, Version: "1", Name: "custom", Description: "Custom report", Directory: "custom", S3Bucket: "bucket", S3Key: "custom", SizeBytes: 10, SHA256: "hash", CreatedAt: time.Now()}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateSkillIcon(ctx, "20000000-0000-4000-8000-000000000002", custom.ExternalID, "bot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace icon update: %v", err)
	}
	custom, err = store.UpdateSkillIcon(ctx, workspace, custom.ExternalID, "bot")
	if err != nil || custom.IconKey != "bot" {
		t.Fatalf("saved icon %+v %v", custom, err)
	}
	updated, _, err := store.CreateSkillVersion(ctx, workspace, custom.ExternalID, SkillVersion{UUID: uuid.New().String(), ExternalID: "skver_custom2", Version: "2", Name: "custom", Description: "Updated report", Directory: "custom", S3Bucket: "bucket", S3Key: "custom2", SizeBytes: 10, SHA256: "hash2", CreatedAt: time.Now()}, new("file-text"))
	if err != nil || updated.IconKey != "file-text" || updated.LatestVersion == nil || *updated.LatestVersion != "2" {
		t.Fatalf("atomic version/icon update %+v %v", updated, err)
	}
	rows, counts, err = store.SkillCatalog(ctx, SkillCatalogQuery{WorkspaceUUID: workspace, Source: "custom", Query: "Updated", Limit: 12})
	if err != nil || len(rows) != 1 || counts.Custom != 1 || rows[0].IconKey != "file-text" {
		t.Fatalf("custom catalog %+v %+v %v", rows, counts, err)
	}

	var imported SystemSkill
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		imported, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{Skill: SystemSkill{UUID: uuid.New().String(), ExternalID: "imported", DisplayTitle: "Imported", IconKey: "image"}, Version: systemSkillRegressionVersion("imported", "auto-one", time.Now())})
		return err
	})
	if err != nil || imported.Listed || imported.LatestVersion == nil || imported.IconKey != "image" {
		t.Fatalf("automatic import %+v %v", imported, err)
	}
	versions, err := store.SystemSkillVersions(ctx, imported.UUID, 10, 0)
	if err != nil || len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("automatic version %+v %v", versions, err)
	}
	if rows, _, err := store.SkillCatalog(ctx, SkillCatalogQuery{WorkspaceUUID: "20000000-0000-4000-8000-000000000002", Query: "Imported", Limit: 12}); err != nil || len(rows) != 0 {
		t.Fatalf("unlisted system skill leaked to workspace catalog: %+v %v", rows, err)
	}

	if _, err := provider.DownTo(ctx, 116); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
}
