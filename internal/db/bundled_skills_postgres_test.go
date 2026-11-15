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

func TestBundledSkillRegistrationRestoresDeletedUploads(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	for _, action := range []string{"delete", "", "list"} {
		t.Run("previous_action_"+action, func(t *testing.T) {
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
			if _, err := provider.UpTo(ctx, 117); err != nil {
				t.Fatal(err)
			}
			store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
			input := SystemSkill{UUID: uuid.New().String(), ExternalID: "xlsx", DisplayTitle: "Uploaded", IconKey: "mic"}
			var previous SystemSkill
			err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
				var err error
				previous, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{Skill: input, Version: systemSkillRegressionVersion("xlsx", "uploaded-1", time.Now())})
				if err != nil || action == "" {
					return err
				}
				previous, err = tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: previous.ExternalID, Action: action})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			base := systemSkillRegressionVersion("xlsx", "bundled-1", time.Now())
			if err := store.SyncBundledSkill(ctx, input, base); err != nil {
				t.Fatal(err)
			}
			current, err := store.GetSystemSkill(ctx, input.ExternalID)
			wantVersion := "uploaded-1"
			if action == "delete" {
				wantVersion = base.Version
			}
			if err != nil || current.UUID != input.UUID || !current.Bundled || current.Listed != (action != "") || current.LatestVersion == nil || *current.LatestVersion != wantVersion || current.IconKey != "mic" {
				t.Fatalf("registration lost lifecycle state: %+v %v", current, err)
			}
			if err := store.SyncBundledSkill(ctx, input, base); err != nil {
				t.Fatal(err)
			}
			restarted, err := store.GetSystemSkill(ctx, input.ExternalID)
			if err != nil || restarted.Listed != current.Listed || *restarted.LatestVersion != *current.LatestVersion {
				t.Fatalf("restart changed lifecycle state: %+v %v", restarted, err)
			}
		})
	}
}

func TestBundledSkillLifecycle(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	version := systemSkillRegressionVersion("preloaded", "bundled-v1", time.Now())
	skill := SystemSkill{UUID: uuid.New().String(), ExternalID: "preloaded", DisplayTitle: "Preloaded", IconKey: "file-text"}
	if err := store.SyncBundledSkill(ctx, skill, version); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || !current.Bundled || !current.Listed || current.LatestVersion == nil {
		t.Fatalf("preload result: %+v %v", current, err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: current.ExternalID, Action: "delete"})
		return err
	})
	if !errors.Is(err, ErrBundledSkillReadOnly) {
		t.Fatalf("bundled base deletion was not rejected: %v", err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		current, err = tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: current.ExternalID, Action: "unlist"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SyncBundledSkill(ctx, skill, version); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || current.Listed {
		t.Fatalf("restart reset listing: %+v %v", current, err)
	}
	versions, err := store.SystemSkillVersions(ctx, current.UUID, 10, 0)
	if err != nil || len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("restart duplicated versions: %+v %v", versions, err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: current.ExternalID, Action: "list"})
		return err
	})
	if err != nil {
		t.Fatalf("cannot relist bundled skill: %v", err)
	}
}

func TestUploadedSystemSkillDeletion(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	var skill SystemSkill
	version := systemSkillRegressionVersion("uploaded", "1", time.Now())
	err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		skill, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{Skill: SystemSkill{UUID: uuid.New().String(), ExternalID: "uploaded", DisplayTitle: "Uploaded"}, Version: version})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: skill.ExternalID, Action: "delete"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSystemSkill(ctx, skill.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted skill remains visible: %v", err)
	}
	if _, err := store.GetSystemSkillVersion(ctx, skill.ExternalID, version.Version); err != nil {
		t.Fatalf("delete removed historical archive reference: %v", err)
	}
}

func TestBundledSkillOverrideSurvivesUpdatesAndRestores(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	base := systemSkillRegressionVersion("preloaded", "bundled-v1", time.Now())
	skill := SystemSkill{UUID: uuid.New().String(), ExternalID: "preloaded", DisplayTitle: "Preloaded", IconKey: "file-text"}
	if err := store.SyncBundledSkill(ctx, skill, base); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	change := func(action string) error {
		return store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			var err error
			current, err = tx.ChangeSystemSkill(ctx, SystemSkillChange{SkillID: skill.ExternalID, Action: action, IconKey: "mic"})
			return err
		})
	}
	override := systemSkillRegressionVersion("preloaded", "uploaded-1", time.Now())
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		current, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{Skill: skill, Version: override})
		return err
	})
	if err != nil || *current.LatestVersion != override.Version || *current.BundledVersion != base.Version {
		t.Fatalf("uploaded override did not become active: %+v %v", current, err)
	}
	if err := change("set_icon"); err != nil {
		t.Fatal(err)
	}
	if err := change("unlist"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncBundledSkill(ctx, skill, base); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != override.Version || current.Listed || current.IconKey != "mic" {
		t.Fatalf("restart reset override or preferences: %+v %v", current, err)
	}
	newBase := systemSkillRegressionVersion("preloaded", "bundled-v2", time.Now())
	if err := store.SyncBundledSkill(ctx, skill, newBase); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != override.Version || *current.BundledVersion != newBase.Version {
		t.Fatalf("project upgrade reset override or missed new baseline: %+v %v", current, err)
	}
	if err := change("delete"); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != newBase.Version || current.Listed || current.IconKey != "mic" || !current.Bundled {
		t.Fatalf("restore did not preserve bundled catalog entry: %+v %v", current, err)
	}
	versions, err := store.SystemSkillVersions(ctx, current.UUID, 10, 0)
	if err != nil || len(versions) != 3 {
		t.Fatalf("restore removed history: %+v %v", versions, err)
	}
	if err := change("delete"); !errors.Is(err, ErrBundledSkillReadOnly) {
		t.Fatalf("base entry can be deleted: %v", err)
	}
	latestBase := systemSkillRegressionVersion("preloaded", "bundled-v3", time.Now())
	if err := store.SyncBundledSkill(ctx, skill, latestBase); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != latestBase.Version || *current.BundledVersion != latestBase.Version || current.Listed {
		t.Fatalf("non-overridden project content did not update: %+v %v", current, err)
	}
}
