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

func TestBundledSkillLifecycle(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 119); err != nil {
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
	actor := uuid.New().String()
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: current.ExternalID, Revision: current.Revision, Action: "delete"})
		return err
	})
	if !errors.Is(err, ErrBundledSkillReadOnly) {
		t.Fatalf("bundled base deletion was not rejected: %v", err)
	}
	_, _, err = store.UpsertBuiltinSkillWithVersion(ctx, BuiltinSkill{ExternalID: skill.ExternalID, DisplayTitle: "Override"}, BuiltinSkillVersion{Version: "seed-override"})
	if !errors.Is(err, ErrBundledSkillReadOnly) {
		t.Fatalf("seed overwrote bundled skill: %v", err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		current, err = tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: current.ExternalID, Revision: current.Revision, Action: "unlist"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := current.Revision
	if err := store.SyncBundledSkill(ctx, skill, version); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || current.Listed || current.Revision != revision {
		t.Fatalf("restart reset listing/revision: %+v %v", current, err)
	}
	versions, err := store.SystemSkillVersions(ctx, current.UUID, 10, 0)
	if err != nil || len(versions) != 1 || versions[0].State != "published" {
		t.Fatalf("restart duplicated versions: %+v %v", versions, err)
	}
	if _, err := store.SoftDeleteMissingBuiltinSkills(ctx, []string{"other"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSystemSkill(ctx, current.ExternalID); err != nil {
		t.Fatalf("seed prune deleted bundled skill: %v", err)
	}
	if _, err := store.SystemSkillVersion(ctx, current.UUID, version.Version); err != nil {
		t.Fatalf("seed prune deleted bundled version: %v", err)
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: current.ExternalID, Revision: current.Revision, Action: "list"})
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
	if _, err := provider.UpTo(ctx, 119); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	actor := uuid.New().String()
	var skill SystemSkill
	version := systemSkillRegressionVersion("uploaded", "1", time.Now())
	err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		skill, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{ActorUUID: actor, Ready: true, Skill: SystemSkill{UUID: uuid.New().String(), ExternalID: "uploaded", DisplayTitle: "Uploaded"}, Version: version})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []int64{skill.Revision - 1, skill.Revision} {
		err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: skill.ExternalID, Revision: revision, Action: "delete"})
			return err
		})
		if revision != skill.Revision {
			if !errors.Is(err, ErrSystemSkillRevision) {
				t.Fatalf("stale delete accepted: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.GetSystemSkill(ctx, skill.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted skill remains visible: %v", err)
	}
	if _, err := store.SystemSkillVersion(ctx, skill.UUID, version.Version); err != nil {
		t.Fatalf("delete removed historical archive reference: %v", err)
	}
}

func TestBundledSkillOverrideSurvivesUpdatesAndRestores(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 119); err != nil {
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
	actor := uuid.New().String()
	change := func(action string, revision int64) error {
		return store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			var err error
			current, err = tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: skill.ExternalID, Revision: revision, Action: action, IconKey: "mic"})
			return err
		})
	}
	override := systemSkillRegressionVersion("preloaded", "uploaded-1", time.Now())
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		var err error
		current, _, err = tx.ImportSystemSkill(ctx, SystemSkillImport{ActorUUID: actor, Skill: skill, Version: override, ExpectedRevision: current.Revision, Ready: true})
		return err
	})
	if err != nil || *current.LatestVersion != override.Version || *current.BundledVersion != base.Version {
		t.Fatalf("uploaded override did not become active: %+v %v", current, err)
	}
	if err := change("set_icon", current.Revision); err != nil {
		t.Fatal(err)
	}
	if err := change("unlist", current.Revision); err != nil {
		t.Fatal(err)
	}
	revision := current.Revision
	if err := store.SyncBundledSkill(ctx, skill, base); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != override.Version || current.Revision != revision || current.Listed || current.IconKey != "mic" {
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
	if err := change("delete", current.Revision-1); !errors.Is(err, ErrSystemSkillRevision) {
		t.Fatalf("stale restore accepted: %v", err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := change("delete", current.Revision); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetSystemSkill(ctx, skill.ExternalID)
	if err != nil || *current.LatestVersion != newBase.Version || current.Listed || current.IconKey != "mic" || !current.Bundled {
		t.Fatalf("restore did not preserve bundled catalog entry: %+v %v", current, err)
	}
	events, err := store.SystemSkillEvents(ctx, current.UUID, 1, 0)
	if err != nil || len(events) != 1 || events[0].Action != "restore_bundled" || events[0].Version != newBase.Version {
		t.Fatalf("restore was not audited: %+v %v", events, err)
	}
	versions, err := store.SystemSkillVersions(ctx, current.UUID, 10, 0)
	if err != nil || len(versions) != 3 {
		t.Fatalf("restore removed history: %+v %v", versions, err)
	}
	revision = current.Revision
	if err := change("delete", revision); !errors.Is(err, ErrBundledSkillReadOnly) {
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
