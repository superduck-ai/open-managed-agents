//go:build integration

package db

import (
	"fmt"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestSystemSkillPublishedPagination(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	mapper := NewSystemSkillMapper(store.mapperDB)
	skill := SystemSkill{UUID: uuid.New().String(), ExternalID: "pagination", DisplayTitle: "Pagination"}
	if _, err := mapper.InsertSkill(ctx, skill); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := range 24 {
		version := systemSkillRegressionVersion(skill.ExternalID, fmt.Sprint(i), base.Add(time.Duration(i)*time.Minute))
		version.SkillUUID, version.SkillExternalID = skill.UUID, skill.ExternalID
		if err := mapper.InsertVersion(ctx, version); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			if _, err := mapper.Publish(ctx, skill.UUID, version.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	first, err := store.SystemSkillVersions(ctx, skill.UUID, 3, 0)
	if err != nil || len(first) != 3 {
		t.Fatalf("first page with lookahead: %+v %v", first, err)
	}
	for i, version := range first {
		if version.State != "published" || version.Version != fmt.Sprint(2-i) {
			t.Fatalf("draft consumed published page: %+v", first)
		}
	}
	last, err := store.SystemSkillVersions(ctx, skill.UUID, 3, 2)
	if err != nil || len(last) != 1 || last[0].Version != "0" {
		t.Fatalf("last published page: %+v %v", last, err)
	}
	empty, err := store.SystemSkillVersions(ctx, skill.UUID, 3, 3)
	if err != nil || len(empty) != 0 {
		t.Fatalf("past final page: %+v %v", empty, err)
	}
}

func TestSystemSkillImportPreservesOmittedIcon(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	actor := uuid.New().String()
	current := SystemSkill{ExternalID: "icons"}
	defaultVersion := ""
	for _, tc := range []struct {
		name string
		icon string
		want string
	}{
		{name: "new default", want: "blocks"},
		{name: "choose icon", icon: "file-text", want: "file-text"},
		{name: "omit on update", want: "file-text"},
		{name: "explicit reset", icon: "blocks", want: "blocks"},
		{name: "choose another icon", icon: "mic", want: "mic"},
		{name: "omit again", want: "mic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := SystemSkillImport{
				ActorUUID: actor, ExpectedRevision: current.Revision, Ready: true,
				Skill:   SystemSkill{UUID: uuid.New().String(), ExternalID: current.ExternalID, DisplayTitle: "Icons", IconKey: tc.icon},
				Version: systemSkillRegressionVersion(current.ExternalID, uuid.New().String(), time.Now()),
			}
			err := store.WithPlatformAdminTx(ctx, func(tx *PlatformAdminTx) error {
				var err error
				current, _, err = tx.ImportSystemSkill(ctx, input)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if current.IconKey != tc.want {
				t.Fatalf("saved icon=%q, want %q", current.IconKey, tc.want)
			}
			if defaultVersion == "" {
				defaultVersion = input.Version.Version
			}
			if current.Listed || current.LatestVersion == nil || *current.LatestVersion != defaultVersion {
				t.Fatalf("import changed visibility or default: %+v", current)
			}
			stored, err := store.GetSystemSkill(ctx, current.ExternalID)
			if err != nil || stored.IconKey != tc.want {
				t.Fatalf("persisted icon=%q, want %q: %v", stored.IconKey, tc.want, err)
			}
		})
	}
}

func systemSkillRegressionVersion(name, version string, created time.Time) SystemSkillVersion {
	return SystemSkillVersion{
		UUID: uuid.New().String(), ExternalID: "skver_" + uuid.New().String(), Version: version,
		Name: name, Description: "Regression fixture", Directory: name,
		S3Bucket: "bucket", S3Key: version, SizeBytes: 10, SHA256: version, CreatedAt: created,
	}
}
