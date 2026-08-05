package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestBuiltinSkillMapperStatements(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	keep := []byte(`["builtin_keep"]`)
	skillParams := upsertBuiltinSkillParams{
		ExternalID:    "builtin_test",
		DisplayTitle:  "Test",
		LatestVersion: "20260805",
		CreatedAt:     now,
	}
	versionParams := upsertBuiltinSkillVersionParams{
		ExternalID:      "builtinver_test",
		SkillID:         7,
		SkillExternalID: skillParams.ExternalID,
		Version:         skillParams.LatestVersion,
		Name:            "test",
		Description:     "Test skill",
		Directory:       "test",
		S3Bucket:        "bucket",
		S3Key:           "key",
		SizeBytes:       10,
		SHA256:          "sha",
		CreatedAt:       now,
	}
	pruneParams := pruneBuiltinSkillsParams{KeepExternalIDsJSON: keep, DeletedAt: now}

	tests := []struct {
		name      string
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		id        string
		kind      yourbatis.StatementKind
		values    []any
		fragments []string
	}{
		{
			name:      "upsert skill",
			statement: builtinSkillMapperUpsertSkillStatement,
			bound:     buildBuiltinSkillMapperUpsertSkill(yourbatis.DialectPostgres, skillParams),
			id:        "BuiltinSkillMapper.UpsertSkill",
			kind:      yourbatis.StatementInsert,
			values: []any{
				skillParams.ExternalID, skillParams.DisplayTitle, skillParams.LatestVersion,
				skillParams.CreatedAt, skillParams.CreatedAt,
			},
			fragments: []string{"INSERT INTO builtin_skills", "ON CONFLICT (external_id)", "RETURNING"},
		},
		{
			name:      "upsert version",
			statement: builtinSkillMapperUpsertVersionStatement,
			bound:     buildBuiltinSkillMapperUpsertVersion(yourbatis.DialectPostgres, versionParams),
			id:        "BuiltinSkillMapper.UpsertVersion",
			kind:      yourbatis.StatementInsert,
			values: []any{
				versionParams.ExternalID, versionParams.SkillID, versionParams.SkillExternalID,
				versionParams.Version, versionParams.Name, versionParams.Description,
				versionParams.Directory, versionParams.S3Bucket, versionParams.S3Key,
				versionParams.SizeBytes, versionParams.SHA256, versionParams.CreatedAt,
			},
			fragments: []string{
				"INSERT INTO builtin_skill_versions", "ON CONFLICT (skill_id, version)",
				"builtin_skill_versions.sha256 = EXCLUDED.sha256", "RETURNING",
			},
		},
		{
			name:      "list skills",
			statement: builtinSkillMapperListSkillsPageStatement,
			bound:     buildBuiltinSkillMapperListSkillsPage(yourbatis.DialectPostgres, 21, 2),
			id:        "BuiltinSkillMapper.ListSkillsPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{21, 2},
			fragments: []string{"ORDER BY created_at DESC, id DESC", "LIMIT $1", "OFFSET $2"},
		},
		{
			name:      "count skills",
			statement: builtinSkillMapperCountSkillsStatement,
			bound:     buildBuiltinSkillMapperCountSkills(yourbatis.DialectPostgres),
			id:        "BuiltinSkillMapper.CountSkills",
			kind:      yourbatis.StatementSelect,
			values:    []any{},
			fragments: []string{"SELECT COUNT(*)", "deleted_at IS NULL"},
		},
		{
			name:      "find skill",
			statement: builtinSkillMapperFindSkillByExternalIDStatement,
			bound:     buildBuiltinSkillMapperFindSkillByExternalID(yourbatis.DialectPostgres, skillParams.ExternalID),
			id:        "BuiltinSkillMapper.FindSkillByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillParams.ExternalID},
			fragments: []string{"external_id = $1", "deleted_at IS NULL"},
		},
		{
			name:      "find skill ID",
			statement: builtinSkillMapperFindSkillIDByExternalIDStatement,
			bound:     buildBuiltinSkillMapperFindSkillIDByExternalID(yourbatis.DialectPostgres, skillParams.ExternalID),
			id:        "BuiltinSkillMapper.FindSkillIDByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillParams.ExternalID},
			fragments: []string{"SELECT id", "external_id = $1"},
		},
		{
			name:      "list versions",
			statement: builtinSkillMapperListVersionsPageStatement,
			bound:     buildBuiltinSkillMapperListVersionsPage(yourbatis.DialectPostgres, versionParams.SkillID, 21, 2),
			id:        "BuiltinSkillMapper.ListVersionsPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{versionParams.SkillID, 21, 2},
			fragments: []string{"skill_id = $1", "ORDER BY created_at DESC, id DESC", "LIMIT $2", "OFFSET $3"},
		},
		{
			name:      "find version",
			statement: builtinSkillMapperFindVersionStatement,
			bound: buildBuiltinSkillMapperFindVersion(
				yourbatis.DialectPostgres,
				versionParams.SkillExternalID,
				versionParams.Version,
			),
			id:        "BuiltinSkillMapper.FindVersion",
			kind:      yourbatis.StatementSelect,
			values:    []any{versionParams.SkillExternalID, versionParams.Version},
			fragments: []string{"skill_external_id = $1", "version = $2", "deleted_at IS NULL"},
		},
		{
			name:      "list missing versions",
			statement: builtinSkillMapperListMissingVersionsStatement,
			bound:     buildBuiltinSkillMapperListMissingVersions(yourbatis.DialectPostgres, keep),
			id:        "BuiltinSkillMapper.ListMissingVersions",
			kind:      yourbatis.StatementSelect,
			values:    []any{keep},
			fragments: []string{"NOT EXISTS", "CAST($1 AS jsonb)", "ORDER BY skill_external_id, version"},
		},
		{
			name:      "delete missing versions",
			statement: builtinSkillMapperSoftDeleteMissingVersionsStatement,
			bound:     buildBuiltinSkillMapperSoftDeleteMissingVersions(yourbatis.DialectPostgres, pruneParams),
			id:        "BuiltinSkillMapper.SoftDeleteMissingVersions",
			kind:      yourbatis.StatementUpdate,
			values:    []any{now, keep},
			fragments: []string{"UPDATE builtin_skill_versions", "deleted_at = $1", "CAST($2 AS jsonb)"},
		},
		{
			name:      "delete missing skills",
			statement: builtinSkillMapperSoftDeleteMissingSkillsStatement,
			bound:     buildBuiltinSkillMapperSoftDeleteMissingSkills(yourbatis.DialectPostgres, pruneParams),
			id:        "BuiltinSkillMapper.SoftDeleteMissingSkills",
			kind:      yourbatis.StatementUpdate,
			values:    []any{now, now, keep},
			fragments: []string{"UPDATE builtin_skills", "deleted_at = $1", "updated_at = $2", "CAST($3 AS jsonb)"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.statement.ID != test.id || test.statement.Kind != test.kind || test.statement.Source == "" {
				t.Fatalf("statement = %+v, want ID %q, kind %q, and source", test.statement, test.id, test.kind)
			}
			if values := test.bound.Values(); !reflect.DeepEqual(values, test.values) {
				t.Fatalf("values = %#v, want %#v", values, test.values)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(test.bound.SQL, fragment) {
					t.Fatalf("SQL = %q, want fragment %q", test.bound.SQL, fragment)
				}
			}
			for _, argument := range test.bound.Args {
				if argument.Sensitive {
					t.Fatalf("non-sensitive argument %q was marked sensitive", argument.Name)
				}
			}
		})
	}
}

func TestBuiltinSkillMapperResultSemantics(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if !errors.Is(err, wantErr) {
			t.Fatalf("FindSkillByExternalID() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("single row zero result", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: builtinSkillMapperTestColumns()})
		_, err := NewBuiltinSkillMapper(executor).UpsertSkill(context.Background(), upsertBuiltinSkillParams{})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpsertSkill() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("single row scan and string UUID", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillMapperTestColumns(),
			rows:    [][]driver.Value{builtinSkillMapperTestRow("builtin_test")},
		})
		row, err := NewBuiltinSkillMapper(executor).UpsertSkill(context.Background(), upsertBuiltinSkillParams{})
		if err != nil || row.UUID != "00000000-0000-4000-8000-000000000001" || row.ExternalID != "builtin_test" {
			t.Fatalf("UpsertSkill() = (%+v, %v)", row, err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		values := builtinSkillMapperTestRow("builtin_test")
		values[5] = "not-a-timestamp"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillMapperTestColumns(),
			rows:    [][]driver.Value{values},
		})
		_, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if err == nil {
			t.Fatal("FindSkillByExternalID() error = nil, want scan error")
		}
	})

	t.Run("many rows empty and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: builtinSkillVersionMapperTestColumns()})
		rows, err := NewBuiltinSkillMapper(emptyExecutor).ListVersionsPage(context.Background(), 7, 20, 0)
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListVersionsPage() = (%+v, %v), want empty result", rows, err)
		}

		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillVersionMapperTestColumns(),
			rows: [][]driver.Value{
				builtinSkillVersionMapperTestRow("1.0.0"),
				builtinSkillVersionMapperTestRow("2.0.0"),
			},
		})
		rows, err = NewBuiltinSkillMapper(executor).ListMissingVersions(context.Background(), []byte(`[]`))
		if err != nil || len(rows) != 2 || rows[1].Version != "2.0.0" {
			t.Fatalf("ListMissingVersions() = (%+v, %v)", rows, err)
		}
	})

	t.Run("scalar zero and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		_, err := NewBuiltinSkillMapper(emptyExecutor).FindSkillIDByExternalID(context.Background(), "builtin_test")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindSkillIDByExternalID() error = %v, want sql.ErrNoRows", err)
		}

		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"count"},
			rows:    [][]driver.Value{{int64(3)}},
		})
		count, err := NewBuiltinSkillMapper(executor).CountSkills(context.Background())
		if err != nil || count != 3 {
			t.Fatalf("CountSkills() = (%d, %v)", count, err)
		}
	})

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("delete failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		err := NewBuiltinSkillMapper(executor).SoftDeleteMissingSkills(context.Background(), pruneBuiltinSkillsParams{})
		if !errors.Is(err, wantErr) {
			t.Fatalf("SoftDeleteMissingSkills() error = %v, want %v", err, wantErr)
		}
	})
}

func builtinSkillMapperTestColumns() []string {
	return []string{
		"id", "uuid", "external_id", "display_title", "latest_version", "created_at", "updated_at", "deleted_at",
	}
}

func builtinSkillMapperTestRow(externalID string) []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		int64(7), "00000000-0000-4000-8000-000000000001", externalID, "Test", "1.0.0", now, now, nil,
	}
}

func builtinSkillVersionMapperTestColumns() []string {
	return []string{
		"id", "uuid", "external_id", "skill_id", "skill_external_id", "version", "name", "description",
		"directory", "s3_bucket", "s3_key", "size_bytes", "sha256", "created_at", "deleted_at",
	}
}

func builtinSkillVersionMapperTestRow(version string) []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		int64(9), "00000000-0000-4000-8000-000000000002", "builtinver_test", int64(7),
		"builtin_test", version, "test", "Test", "test", "bucket", "key", int64(10), "sha", now, nil,
	}
}
