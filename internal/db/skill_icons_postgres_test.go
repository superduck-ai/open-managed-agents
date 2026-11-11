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
	if _, err := provider.UpTo(ctx, 116); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
 INSERT INTO builtin_skills (external_id,display_title,latest_version,listed) VALUES ('docx','Word','1',false);
 INSERT INTO builtin_skill_versions (external_id,skill_uuid,skill_external_id,version,name,description,directory,state,s3_bucket,s3_key,size_bytes,sha256)
 SELECT 'skver_icon',uuid,external_id,'1','docx','Reports','docx','published','bucket','archive',10,'hash' FROM builtin_skills WHERE external_id='docx';
 INSERT INTO builtin_skills (external_id,display_title,listed) VALUES ('legacy-draft','Legacy',false);
 INSERT INTO builtin_skill_versions (external_id,skill_uuid,skill_external_id,version,name,description,directory,state,s3_bucket,s3_key,size_bytes,sha256)
 SELECT 'skver_legacy',uuid,external_id,'1','legacy-draft','Unpublished','legacy','draft','bucket','legacy',10,'legacy-hash' FROM builtin_skills WHERE external_id='legacy-draft';
 `); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 118); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	legacy, err := store.GetSystemSkill(ctx, "legacy-draft")
	if err != nil || legacy.LatestVersion != nil || legacy.Listed {
		t.Fatalf("migration changed legacy visibility: %+v %v", legacy, err)
	}
	legacyVersions, err := store.SystemSkillVersions(ctx, legacy.UUID, 10, 0)
	if err != nil || len(legacyVersions) != 0 {
		t.Fatalf("draft entered available versions: %+v %v", legacyVersions, err)
	}
	legacyVersion, err := store.SystemSkillVersion(ctx, legacy.UUID, "1")
	if err != nil || legacyVersion.State != "draft" {
		t.Fatalf("migration published historical data: %+v %v", legacyVersion, err)
	}
	skill, err := store.GetSystemSkill(ctx, "docx")
	if err != nil || skill.IconKey != "file-text" || skill.Listed || skill.LatestVersion == nil || *skill.LatestVersion != "1" {
		t.Fatalf("migrated skill: %+v %v", skill, err)
	}
	versions, err := store.SystemSkillVersions(ctx, skill.UUID, 10, 0)
	if err != nil || len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("migrated versions: %+v %v", versions, err)
	}
	for _, workspace := range []string{"20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002"} {
		rows, counts, err := store.SkillCatalog(ctx, SkillCatalogQuery{WorkspaceUUID: workspace, Limit: 12})
		if err != nil || len(rows) != 0 || counts.All != 0 {
			t.Fatalf("unlisted leaked: %+v %+v %v", rows, counts, err)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE builtin_skills SET listed=true WHERE external_id='docx'`); err != nil {
		t.Fatal(err)
	}
	rows, counts, err := store.SkillCatalog(ctx, SkillCatalogQuery{WorkspaceUUID: "20000000-0000-4000-8000-000000000001", Query: "Reports", Limit: 12})
	if err != nil || len(rows) != 1 || counts.Builtin != 1 || rows[0].Description != "Reports" || rows[0].IconKey != "file-text" {
		t.Fatalf("listed catalog: %+v %+v %v", rows, counts, err)
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
	actor := uuid.New().String()
	var imported SystemSkill
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		imported, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{ActorUUID: actor, Ready: true, Skill: SystemSkill{UUID: uuid.New().String(), ExternalID: "imported", DisplayTitle: "Imported", IconKey: "image"}, Version: SystemSkillVersion{UUID: uuid.New().String(), ExternalID: "skver_imported", Version: "auto-one", Name: "imported", Description: "Imported report", Directory: "imported", S3Bucket: "bucket", S3Key: "imported", SizeBytes: 10, SHA256: "imported-hash", CreatedAt: time.Now()}})
		return err
	})
	if err != nil || imported.Listed || imported.LatestVersion == nil || imported.IconKey != "image" {
		t.Fatalf("automatic import %+v %v", imported, err)
	}
	versions, err = store.SystemSkillVersions(ctx, imported.UUID, 10, 0)
	if err != nil || len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("automatic version %+v %v", versions, err)
	}
	if _, err := provider.DownTo(ctx, 116); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
}
