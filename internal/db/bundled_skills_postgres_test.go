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
	if _, err := provider.UpTo(ctx, 118); err != nil {
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
	for _, action := range []string{"delete", "set_icon", "set_default", "publish"} {
		err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
			_, err := tx.ChangeSystemSkill(ctx, SystemSkillChange{ActorUUID: actor, SkillID: current.ExternalID, Revision: current.Revision, Action: action, Version: version.Version, IconKey: "mic"})
			return err
		})
		if !errors.Is(err, ErrBundledSkillReadOnly) {
			t.Fatalf("bundled %s was not rejected: %v", action, err)
		}
	}
	err = store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
		_, _, err := tx.ImportSystemSkill(ctx, SystemSkillImport{ActorUUID: actor, ExpectedRevision: current.Revision, Skill: skill, Version: version, Ready: true})
		return err
	})
	if !errors.Is(err, ErrBundledSkillReadOnly) {
		t.Fatalf("bundled import was not rejected: %v", err)
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
	if _, err := provider.UpTo(ctx, 118); err != nil {
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
