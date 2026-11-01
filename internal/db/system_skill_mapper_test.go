package db

import (
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSystemSkillMapperBindings(t *testing.T) {
	for _, status := range []string{"", "listed", "unlisted", "draft"} {
		query := SystemSkillQuery{Query: "%_ search", Status: status, Limit: 21, Offset: 20}
		bound := buildSystemSkillMapperList(yourbatis.DialectPostgres, query)
		want := []any{query.Query, query.Query, 21, 20}
		values := make([]any, len(bound.Args))
		for i, p := range bound.Args {
			values[i] = p.Value
		}
		if !reflect.DeepEqual(values, want) {
			t.Fatalf("list values=%v want=%v", values, want)
		}
		for _, fragment := range []string{"deleted_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $3 OFFSET $4"} {
			if !strings.Contains(bound.SQL, fragment) {
				t.Errorf("missing %s: %s", fragment, bound.SQL)
			}
		}
	}
	for _, lock := range []bool{false, true} {
		bound := buildSystemSkillMapperFind(yourbatis.DialectPostgres, "demo", lock)
		if strings.Contains(bound.SQL, "FOR UPDATE") != lock {
			t.Errorf("lock=%v query=%s", lock, bound.SQL)
		}
	}
	skill := SystemSkill{UUID: "11111111-1111-4111-8111-111111111111", Revision: 4, LatestVersion: new("2.0.0"), Listed: true}
	bound := buildSystemSkillMapperUpdateSkill(yourbatis.DialectPostgres, skill)
	for _, fragment := range []string{"revision = revision + 1", "WHERE uuid = $3 AND revision = $4", "deleted_at IS NULL"} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Errorf("missing update invariant %s: %s", fragment, bound.SQL)
		}
	}
	version := buildSystemSkillMapperPublish(yourbatis.DialectPostgres, skill.UUID, "2.0.0")
	if !strings.Contains(version.SQL, "state = 'draft'") {
		t.Errorf("publish must only change drafts: %s", version.SQL)
	}
	archive := buildSystemSkillMapperHasArchive(yourbatis.DialectPostgres, "unique-object")
	if len(archive.Args) != 1 || archive.Args[0].Value != "unique-object" {
		t.Fatal("archive reference check does not bind key")
	}
}

func TestSystemSkillMapperWritesAndPages(t *testing.T) {
	skill := SystemSkill{UUID: "11111111-1111-4111-8111-111111111111", ExternalID: "demo", DisplayTitle: "Demo"}
	version := SystemSkillVersion{UUID: "22222222-2222-4222-8222-222222222222", ExternalID: "skver_demo", SkillUUID: skill.UUID, SkillExternalID: skill.ExternalID, Version: "1", Name: "demo", Description: "Description", Directory: "demo", S3Bucket: "bucket", S3Key: "key", SizeBytes: 100, SHA256: "hash"}
	event := SystemSkillEvent{SkillUUID: skill.UUID, ActorUUID: "33333333-3333-4333-8333-333333333333", Action: "publish", Version: "1"}
	for _, tc := range []struct {
		bound    yourbatis.BoundSQL
		values   []any
		fragment string
	}{
		{buildSystemSkillMapperInsertSkill(yourbatis.DialectPostgres, skill), []any{skill.UUID, skill.ExternalID, skill.DisplayTitle}, "ON CONFLICT (external_id) DO NOTHING"},
		{buildSystemSkillMapperInsertVersion(yourbatis.DialectPostgres, version), []any{version.UUID, version.ExternalID, version.SkillUUID, version.SkillExternalID, version.Version, version.Name, version.Description, version.Directory, version.S3Bucket, version.S3Key, version.SizeBytes, version.SHA256, version.CreatedAt}, "'draft'"},
		{buildSystemSkillMapperVersions(yourbatis.DialectPostgres, skill.UUID, 21, 20), []any{skill.UUID, 21, 20}, "ORDER BY created_at DESC, uuid DESC"},
		{buildSystemSkillMapperVersion(yourbatis.DialectPostgres, skill.UUID, "1"), []any{skill.UUID, "1"}, "deleted_at IS NULL"},
		{buildSystemSkillMapperEvents(yourbatis.DialectPostgres, skill.UUID, 21, 20), []any{skill.UUID, 21, 20}, "ORDER BY created_at DESC, uuid DESC"},
		{buildSystemSkillMapperRecord(yourbatis.DialectPostgres, event), []any{event.SkillUUID, event.ActorUUID, event.Action, event.Version}, "INSERT INTO system_skill_events"},
	} {
		values := make([]any, len(tc.bound.Args))
		for i, arg := range tc.bound.Args {
			values[i] = arg.Value
		}
		if !reflect.DeepEqual(values, tc.values) || !strings.Contains(tc.bound.SQL, tc.fragment) {
			t.Errorf("SQL or bindings mismatch: %s %v", tc.bound.SQL, values)
		}
	}
}

func TestSystemSkillMapperStatementContracts(t *testing.T) {
	for _, tc := range []struct {
		statement yourbatis.Statement
		name      string
		kind      yourbatis.StatementKind
	}{
		{systemSkillMapperListStatement, "List", yourbatis.StatementSelect},
		{systemSkillMapperFindStatement, "Find", yourbatis.StatementSelect},
		{systemSkillMapperInsertSkillStatement, "InsertSkill", yourbatis.StatementInsert},
		{systemSkillMapperUpdateSkillStatement, "UpdateSkill", yourbatis.StatementUpdate},
		{systemSkillMapperVersionsStatement, "Versions", yourbatis.StatementSelect},
		{systemSkillMapperVersionStatement, "Version", yourbatis.StatementSelect},
		{systemSkillMapperInsertVersionStatement, "InsertVersion", yourbatis.StatementInsert},
		{systemSkillMapperPublishStatement, "Publish", yourbatis.StatementUpdate},
		{systemSkillMapperEventsStatement, "Events", yourbatis.StatementSelect},
		{systemSkillMapperRecordStatement, "Record", yourbatis.StatementInsert},
		{systemSkillMapperHasArchiveStatement, "HasArchive", yourbatis.StatementSelect},
	} {
		if tc.statement.ID != "SystemSkillMapper."+tc.name || tc.statement.Kind != tc.kind {
			t.Errorf("statement contract mismatch: %+v", tc.statement)
		}
	}
}
